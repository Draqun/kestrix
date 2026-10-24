package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"kestrix/internal/config"
	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/redact"
	"kestrix/internal/runner"
)

// cmdRunner implements `kestrix runner`: a poll-loop that claims jobs from a
// Kestrix server's runner-job endpoints and executes them locally, using
// this same binary's existing internal/runner Executor implementations -
// see remote-runner-registration proposal.md Impact (single-binary
// convention) and design.md D7 (the runner's own thin adapter, never the
// control-plane's Options).
func cmdRunner(args []string) {
	fs := flag.NewFlagSet("runner", flag.ExitOnError)
	server := fs.String("server", "", "Kestrix server base URL, e.g. https://kestrix.example.com (env: KESTRIX_RUNNER_SERVER)")
	token := fs.String("token", "", "runner bearer token, issued by POST /api/runners (env: KESTRIX_RUNNER_TOKEN)")
	mode := fs.String("mode", "", "execution backend: local or docker (env: KESTRIX_RUNNER_MODE, default local)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	resolvedServer, resolvedToken, resolvedMode := resolveRunnerFlags(*server, *token, *mode, config.Current())
	if resolvedServer == "" || resolvedToken == "" {
		fmt.Fprintln(os.Stderr, "kestrix runner: --server and --token (or KESTRIX_RUNNER_SERVER / KESTRIX_RUNNER_TOKEN) are required")
		os.Exit(2)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	fmt.Fprintf(os.Stderr, "kestrix runner: polling %s (mode=%s)\n", resolvedServer, resolvedMode)
	if err := runPollLoop(context.Background(), client, resolvedServer, resolvedToken, resolvedMode); err != nil {
		fmt.Fprintf(os.Stderr, "kestrix runner: %v\n", err)
		os.Exit(1)
	}
}

// resolveRunnerFlags applies --server/--token/--mode's precedence over
// KESTRIX_RUNNER_SERVER/TOKEN/MODE (flag wins, then env, then "local" for
// mode) - pulled out of cmdRunner so this precedence is unit-testable
// directly: cmdRunner itself can only be driven as a subprocess (it either
// os.Exits or blocks forever in runPollLoop), which would make asserting on
// precedence alone needlessly heavy (REVIEW.md R9).
func resolveRunnerFlags(server, token, mode string, env *config.Environment) (resolvedServer, resolvedToken, resolvedMode string) {
	resolvedServer, resolvedToken, resolvedMode = server, token, mode
	if resolvedServer == "" {
		resolvedServer = env.RunnerServer
	}
	if resolvedToken == "" {
		resolvedToken = env.RunnerToken
	}
	if resolvedMode == "" {
		resolvedMode = env.RunnerMode
	}
	if resolvedMode == "" {
		resolvedMode = "local"
	}
	resolvedServer = strings.TrimRight(resolvedServer, "/")
	return resolvedServer, resolvedToken, resolvedMode
}

// pollErrorRetryDelay bounds how long runPollLoop waits after a poll error
// before retrying. A package variable, not a constant, so a test isn't
// forced to wait the real interval - the same pattern
// internal/web's runnerLongPollInterval uses.
var pollErrorRetryDelay = 5 * time.Second

// errRunnerRejected marks a poll the server answered 401: the runner's token
// is unknown or revoked, so no retry can succeed (remote-run-ux design.md D4).
// Its message carries the server's own.
var errRunnerRejected = errors.New("server rejected the runner token")

// runPollLoop polls GET /api/runner-jobs/next until ctx is done (nil) or the
// server rejects the runner's token (errRunnerRejected); every other poll
// failure is logged and retried. The server itself does the bounded
// long-poll wait (tasks.md 3.2), so a 204 here means "nothing pending right
// now" and the loop re-polls immediately rather than sleeping - it already
// just spent up to that long-poll interval waiting.
func runPollLoop(ctx context.Context, client *http.Client, baseURL, token, mode string) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		job, status, err := pollNextJob(ctx, client, baseURL, token)
		if errors.Is(err, errRunnerRejected) {
			return err
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "kestrix runner: poll error: %v\n", err)
			time.Sleep(pollErrorRetryDelay)
			continue
		}
		if status == http.StatusNoContent || job == nil {
			continue
		}
		fmt.Fprintf(os.Stderr, "kestrix runner: claimed job %d (task %q, epoch %d)\n", job.ID, job.TaskName, job.ClaimEpoch)
		runJob(ctx, client, baseURL, token, mode, job)
	}
}

// pollNextJob makes one GET /api/runner-jobs/next call.
func pollNextJob(ctx context.Context, client *http.Client, baseURL, token string) (*domain.RemoteJob, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/runner-jobs/next", nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil, resp.StatusCode, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, resp.StatusCode, fmt.Errorf("%w: %s", errRunnerRejected, serverMessage(body))
		}
		return nil, resp.StatusCode, fmt.Errorf("unexpected status %s: %s", resp.Status, string(body))
	}
	var job domain.RemoteJob
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode job: %w", err)
	}
	return &job, resp.StatusCode, nil
}

// serverMessage is the "error" of a JSON error body as the control plane
// writes it, or the trimmed body itself when it is not one.
func serverMessage(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return e.Error
	}
	return strings.TrimSpace(string(body))
}

// runJob unpacks job's source archive, executes its one task through
// internal/runner's existing Executor (selected by this runner's own
// --mode, never anything the job payload names - design.md D3), and
// reports the outcome. Every failure still reports completion so the job
// does not sit "claimed" until it times out and gets reclaimed for nothing.
func runJob(ctx context.Context, client *http.Client, baseURL, token, mode string, job *domain.RemoteJob) {
	workDir, err := os.MkdirTemp("", "kestrix-runner-job-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "kestrix runner: create workdir: %v\n", err)
		msg := "runner: create workdir: " + err.Error()
		reportComplete(ctx, client, baseURL, token, job.ID, job.ClaimEpoch, "failed", msg, msg)
		return
	}
	defer os.RemoveAll(workDir)

	if err := unpackArchive(job.Payload.Archive, workDir); err != nil {
		fmt.Fprintf(os.Stderr, "kestrix runner: unpack archive: %v\n", err)
		msg := "runner: unpack archive: " + err.Error()
		reportComplete(ctx, client, baseURL, token, job.ID, job.ClaimEpoch, "failed", msg, msg)
		return
	}
	for _, artArchive := range job.Payload.Artifacts {
		if err := unpackArchive(artArchive, workDir); err != nil {
			fmt.Fprintf(os.Stderr, "kestrix runner: unpack artifact: %v\n", err)
			msg := "runner: unpack artifact: " + err.Error()
			reportComplete(ctx, client, baseURL, token, job.ID, job.ClaimEpoch, "failed", msg, msg)
			return
		}
	}

	project := model.Project{
		Repos:        job.Payload.Repos,
		Tasks:        []model.Task{job.Payload.Task},
		DefaultImage: job.Payload.DefaultImage,
	}
	tw := &traceWriter{client: client, baseURL: baseURL, token: token, jobID: job.ID, epoch: job.ClaimEpoch}
	// Redact here, where the stream is still whole: the server redacts each
	// trace chunk too, but a secret split across two chunks only matches
	// on this side (remote-runner-fixes design.md D1).
	out := redact.NewWriter(tw, job.Payload.SecretValues)
	opts := buildRunnerOptions(workDir, job.Payload, mode, out)

	tr, runErr := runner.RunOne(ctx, project, job.Payload.Task.Name, opts)
	_ = out.Flush() // traceWriter never fails a write; see its doc
	status := tr.Status
	if status == "" {
		status = "failed"
	}
	taskError := tr.Error
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "kestrix runner: task %q: %v\n", job.Payload.Task.Name, runErr)
		if taskError == "" {
			taskError = runErr.Error()
		}
	}
	if status == "passed" && len(job.Payload.Task.Artifacts) > 0 {
		archive, err := archiveDeclaredArtifacts(workDir, job.Payload.Repos, job.Payload.Task)
		if err != nil {
			fmt.Fprintf(os.Stderr, "kestrix runner: archive artifacts: %v\n", err)
			status = "failed"
			taskError = err.Error()
		} else if err := uploadArtifacts(ctx, client, baseURL, token, job.ID, job.ClaimEpoch, archive); err != nil {
			fmt.Fprintf(os.Stderr, "kestrix runner: %v\n", err)
			status = "failed"
			taskError = err.Error()
		}
	}
	// output is left empty: every byte of it already streamed via
	// tw's PATCH .../trace calls during execution (LocalExecutor/
	// DockerExecutor tee to opts.Stdout as they run) - resending the whole
	// aggregate here would duplicate it, since CompleteJob appends whatever
	// this call's own output carries.
	// taskError carries why a task failed when it never produced output to
	// say so itself - e.g. a missing shell (design.md D4).
	if err := reportComplete(ctx, client, baseURL, token, job.ID, job.ClaimEpoch, status, "", redact.Values(taskError, job.Payload.SecretValues)); err != nil {
		fmt.Fprintf(os.Stderr, "kestrix runner: report completion for job %d: %v\n", job.ID, err)
	}
}

// buildRunnerOptions constructs the runner.Options RunOne executes with,
// entirely from the job payload and this runner's own local workDir/mode -
// design.md D7: never the control-plane's Options. In particular it leaves
// SourceSessions, SourceAcquirer, and CredentialResolver zero: the runner
// never reads control-plane config, resolves a credential, or acquires
// source itself (the archive already IS the acquired source, D6).
func buildRunnerOptions(workDir string, payload domain.RemoteJobPayload, mode string, out io.Writer) runner.Options {
	return runner.Options{
		WorkDir:         workDir,
		SourcesPrepared: true,
		Variables:       flatVariableResolver(payload.Variables),
		Stdout:          out,
		Stderr:          out,
		Mode:            mode,
	}
}

// reportComplete makes one PUT /api/runner-jobs/{id}/complete call.
// taskError may be empty.
func reportComplete(ctx context.Context, client *http.Client, baseURL, token string, jobID, epoch int, status, output, taskError string) error {
	body, _ := json.Marshal(map[string]any{"epoch": epoch, "status": status, "output": output, "error": taskError})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		baseURL+"/api/runner-jobs/"+strconv.Itoa(jobID)+"/complete", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("unexpected status %s: %s", resp.Status, string(respBody))
	}
	return nil
}

// traceWriter streams task output to PATCH /api/runner-jobs/{id}/trace as
// it is written, tracking its own offset (tasks.md 3.3). A push failure is
// logged and the write still succeeds from the caller's (the executor's)
// point of view - a lost trace chunk must never abort task execution; the
// task's own exit status is what CompleteJob ultimately records.
type traceWriter struct {
	client         *http.Client
	baseURL, token string
	jobID, epoch   int
	mu             sync.Mutex
	offset         int
}

func (w *traceWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	body, _ := json.Marshal(map[string]any{"epoch": w.epoch, "offset": w.offset, "chunk": string(p)})
	req, err := http.NewRequest(http.MethodPatch,
		w.baseURL+"/api/runner-jobs/"+strconv.Itoa(w.jobID)+"/trace", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "kestrix runner: build trace request: %v\n", err)
		return len(p), nil
	}
	req.Header.Set("Authorization", "Bearer "+w.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kestrix runner: push trace: %v\n", err)
		return len(p), nil
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		w.offset += len(p)
	} else {
		fmt.Fprintf(os.Stderr, "kestrix runner: trace push rejected: %s\n", resp.Status)
	}
	return len(p), nil
}

// flatVariableResolver adapts a single already-resolved variable map (the
// job payload's own Variables) to domain.VariableResolver: the runner has
// exactly one task's worth of variables, already merged server-side, so
// repo/job/env scoping no longer applies.
type flatVariableResolver map[string]string

func (f flatVariableResolver) Get(repo, job, env string) map[string]string {
	return map[string]string(f)
}

// unpackArchive extracts a tar.gz built by internal/service.archiveWorkDir
// into destDir.
func unpackArchive(data []byte, destDir string) error {
	if len(data) == 0 {
		return nil
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}
		target, err := safeJoin(destDir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := safeSymlinkTarget(destDir, target, hdr.Linkname); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		default:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode&0o777))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
}

// safeJoin joins destDir and name, rejecting a tar entry that would escape
// destDir (a hardened archive extractor never trusts entry paths verbatim).
func safeJoin(destDir, name string) (string, error) {
	target := filepath.Join(destDir, name)
	rel, err := filepath.Rel(destDir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry %q escapes the extraction directory", name)
	}
	return target, nil
}

// safeSymlinkTarget rejects a tar symlink entry whose target (linkname)
// resolves outside destDir - safeJoin alone only validates an entry's own
// name, not where a symlink it creates would point, so a crafted absolute
// or "../"-escaping Linkname could otherwise let a later archive entry
// write through it to a path outside destDir (tar-slip).
func safeSymlinkTarget(destDir, entryTarget, linkname string) error {
	var resolved string
	if filepath.IsAbs(linkname) {
		resolved = filepath.Clean(linkname)
	} else {
		resolved = filepath.Join(filepath.Dir(entryTarget), linkname)
	}
	rel, err := filepath.Rel(destDir, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("symlink target %q escapes the extraction directory", linkname)
	}
	return nil
}

func uploadArtifacts(ctx context.Context, client *http.Client, baseURL, token string, jobID, epoch int, archive []byte) error {
	url := fmt.Sprintf("%s/api/runner-jobs/%d/artifacts?epoch=%d", baseURL, jobID, epoch)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("create artifact request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/gzip")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("upload artifacts: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	msg := serverMessage(body)
	if msg != "" {
		return fmt.Errorf("upload artifacts: %s", msg)
	}
	return fmt.Errorf("upload artifacts: %s", resp.Status)
}

func archiveDeclaredArtifacts(workDir string, repos []domain.Repo, task domain.Task) ([]byte, error) {
	var repoPath string
	for _, r := range repos {
		if r.Name == task.Execute.Repo {
			repoPath = r.Path
			break
		}
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, art := range task.Artifacts {
		targetPath := filepath.Join(workDir, repoPath, art)
		baseRel := filepath.Clean(filepath.Join(repoPath, art))
		if baseRel == "." {
			baseRel = ""
		}

		info, err := os.Lstat(targetPath)
		if err != nil {
			return nil, fmt.Errorf("artifact %q: %w", art, err)
		}

		if !info.IsDir() {
			if err := addTarEntry(tw, targetPath, baseRel, info); err != nil {
				return nil, err
			}
			continue
		}

		dirRel := baseRel
		if dirRel != "" && !strings.HasSuffix(dirRel, "/") {
			dirRel += "/"
		}
		if dirRel != "" {
			hdr, _ := tar.FileInfoHeader(info, "")
			hdr.Name = dirRel
			if err := tw.WriteHeader(hdr); err != nil {
				return nil, err
			}
		}

		err = filepath.Walk(targetPath, func(path string, walkInfo os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path == targetPath {
				return nil
			}
			subRel, err := filepath.Rel(targetPath, path)
			if err != nil {
				return err
			}
			entryName := filepath.ToSlash(filepath.Join(baseRel, subRel))
			if walkInfo.IsDir() && !strings.HasSuffix(entryName, "/") {
				entryName += "/"
			}
			return addTarEntry(tw, path, entryName, walkInfo)
		})
		if err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func addTarEntry(tw *tar.Writer, path, entryName string, info os.FileInfo) error {
	entryName = filepath.ToSlash(entryName)
	if info.IsDir() {
		if !strings.HasSuffix(entryName, "/") {
			entryName += "/"
		}
		hdr, _ := tar.FileInfoHeader(info, "")
		hdr.Name = entryName
		return tw.WriteHeader(hdr)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		link, _ := os.Readlink(path)
		hdr, _ := tar.FileInfoHeader(info, link)
		hdr.Name = entryName
		return tw.WriteHeader(hdr)
	}
	hdr, _ := tar.FileInfoHeader(info, "")
	hdr.Name = entryName
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(tw, f)
	return err
}
