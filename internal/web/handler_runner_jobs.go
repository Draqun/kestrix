package web

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"kestrix/internal/domain"
	"kestrix/internal/redact"
)

// runnerLongPollInterval bounds how long GET /api/runner-jobs/next blocks
// waiting for a pending job before answering 204 - see tasks.md 3.2. It
// polls the store at runnerLongPollTick, a cheap interval given Phase 1's
// scale (two runners, one project - design.md Risks). Package variables,
// not constants, so a unit test isn't forced to wait the real interval -
// the same pattern service.CancelWaitBuffer uses.
var (
	runnerLongPollInterval = 20 * time.Second
	runnerLongPollTick     = 500 * time.Millisecond
)

// invalidRunnerTokenMessage is the 401 for a well-formed bearer token the
// runner store does not accept. It is the same for an unknown and a revoked
// token, so it does not reveal whether a token ever existed.
const invalidRunnerTokenMessage = "invalid or revoked runner token"

// runnerIDContextKey is the context key requireRunnerAuth stores an
// authenticated runner's ID under.
type runnerIDContextKey struct{}

func runnerIDFromContext(ctx context.Context) (int, bool) {
	id, ok := ctx.Value(runnerIDContextKey{}).(int)
	return id, ok
}

// runnerPollLimiter bounds how many concurrent GET /api/runner-jobs/next
// long-polls one authenticated runner token may hold open at once
// (design.md D4) - a misbehaving or compromised token then has a
// documented ceiling, not none.
type runnerPollLimiter struct {
	mu    sync.Mutex
	inUse map[int]int
}

func newRunnerPollLimiter() *runnerPollLimiter {
	return &runnerPollLimiter{inUse: make(map[int]int)}
}

// acquire reports whether runnerID is under max concurrent polls and, if
// so, reserves one slot. Call release when the poll returns.
func (l *runnerPollLimiter) acquire(runnerID, max int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inUse[runnerID] >= max {
		return false
	}
	l.inUse[runnerID]++
	return true
}

func (l *runnerPollLimiter) release(runnerID int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.inUse[runnerID]--
	if l.inUse[runnerID] <= 0 {
		delete(l.inUse, runnerID)
	}
}

// requireRunnerAuth authenticates the runner-job route group with a bearer
// token resolved through RunnerStore.AuthenticateRunner - a separate
// credential space from a user session or an /api token (access-control
// spec's "Runner token does not authenticate /api"): it never calls
// RequireAuth, never resolves an account or role, and a user session
// presented here is rejected the same as any other unrecognized value.
//
// The response says what failed (remote-run-ux design.md D4): a missing or
// non-bearer header is missingCredentialMessage, a rejected token is
// invalidRunnerTokenMessage, and a store failure is a 500 - `kestrix runner`
// exits on 401, so a database blip must never look like a revoked token.
func (s *Server) requireRunnerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) || s.runnerStore == nil {
			writeError(w, http.StatusUnauthorized, missingCredentialMessage)
			return
		}
		token := header[len(prefix):]
		runnerID, ok, err := s.runnerStore.AuthenticateRunner(token)
		if err != nil {
			slog.Warn("could not check runner token", "err", redact.Err(err, s.knownSecrets()))
			writeError(w, http.StatusInternalServerError, "could not check runner token")
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, invalidRunnerTokenMessage)
			return
		}
		ctx := context.WithValue(r.Context(), runnerIDContextKey{}, runnerID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// handleRunnerJobNext is a bounded long-poll: it claims the oldest pending
// job for the authenticated runner, blocking up to runnerLongPollInterval
// before answering 204 when none arrives - never an error (tasks.md 3.2).
func (s *Server) handleRunnerJobNext(w http.ResponseWriter, r *http.Request) {
	runnerID, ok := runnerIDFromContext(r.Context())
	if !ok || s.remoteJobStore == nil {
		writeError(w, http.StatusInternalServerError, "runner jobs not configured")
		return
	}

	if s.runnerPollLimiter != nil {
		max := s.runnerJobMaxConcurrentPolls()
		if !s.runnerPollLimiter.acquire(runnerID, max) {
			writeError(w, http.StatusTooManyRequests, "too many concurrent polls for this runner")
			return
		}
		defer s.runnerPollLimiter.release(runnerID)
	}

	deadline := time.Now().Add(runnerLongPollInterval)
	ticker := time.NewTicker(runnerLongPollTick)
	defer ticker.Stop()
	for {
		job, err := s.remoteJobStore.ClaimNextJob(runnerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "claim job")
			return
		}
		if job != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(job)
			return
		}
		if time.Now().After(deadline) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

// runnerTraceRequest is the body PATCH /api/runner-jobs/{id}/trace accepts.
type runnerTraceRequest struct {
	Epoch  int    `json:"epoch"`
	Offset *int   `json:"offset"`
	Chunk  string `json:"chunk"`
}

// handleRunnerJobTrace implements PATCH /api/runner-jobs/{id}/trace
// (tasks.md 3.3): offset-based, redacted before storage, rejected as a
// no-op when the epoch no longer matches or the job is not this runner's own.
func (s *Server) handleRunnerJobTrace(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	runnerID, ok := runnerIDFromContext(r.Context())
	if !ok || s.remoteJobStore == nil {
		writeError(w, http.StatusInternalServerError, "runner jobs not configured")
		return
	}
	var req runnerTraceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Offset == nil {
		writeError(w, http.StatusBadRequest, "offset is required")
		return
	}
	secrets, err := s.jobSecrets(jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load job secrets")
		return
	}
	chunk := redact.Values(req.Chunk, secrets)
	maxOutputBytes := 0
	if s.runService != nil {
		maxOutputBytes = s.runService.MaxOutputBytes
	}
	traced, err := s.remoteJobStore.AppendJobTrace(jobID, req.Epoch, *req.Offset, chunk, maxOutputBytes, &runnerID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !traced {
		writeError(w, http.StatusConflict, "stale epoch, job not owned by this runner, or job already terminal")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// jobSecrets returns every value text a runner sends back for jobID must
// have redacted before it is stored: this server's own secrets plus the
// resolved runtime secrets of the job's run (remote-runner-fixes design.md
// D1) - knownSecrets alone never covers a pipeline's secret_ref values.
func (s *Server) jobSecrets(jobID int) ([]string, error) {
	values, err := s.remoteJobStore.RemoteJobSecretValues(jobID)
	if err != nil {
		return nil, err
	}
	return append(s.knownSecrets(), values...), nil
}

// maxRunnerTaskErrorBytes bounds the error a completion report may attach
// to a task result: it is runner-supplied text, stored per task.
const maxRunnerTaskErrorBytes = 4096

// runnerCompleteRequest is the body PUT /api/runner-jobs/{id}/complete
// accepts. Error is optional; an older runner never sends it.
type runnerCompleteRequest struct {
	Epoch  int    `json:"epoch"`
	Status string `json:"status"`
	Output string `json:"output"`
	Error  string `json:"error"`
}

// handleRunnerJobComplete implements PUT /api/runner-jobs/{id}/complete
// (tasks.md 3.4): a thin adapter into RunService.CompleteRemoteJob, which
// owns the epoch/ownership/terminal-run fencing and the dependency-resumption
// step.
func (s *Server) handleRunnerJobComplete(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	runnerID, ok := runnerIDFromContext(r.Context())
	if !ok || s.runService == nil || s.remoteJobStore == nil {
		writeError(w, http.StatusInternalServerError, "run service not configured")
		return
	}
	var req runnerCompleteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	secrets, err := s.jobSecrets(jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load job secrets")
		return
	}
	output := redact.Values(req.Output, secrets)
	// Redact before cutting, so the cut can never leave part of a secret
	// that no longer matches its whole value.
	taskError := redact.Values(req.Error, secrets)
	if len(taskError) > maxRunnerTaskErrorBytes {
		cut := maxRunnerTaskErrorBytes
		for cut > 0 && !utf8.RuneStart(taskError[cut]) {
			cut-- // never split a multi-byte character
		}
		taskError = taskError[:cut]
	}
	completed, err := s.runService.CompleteRemoteJob(jobID, req.Epoch, req.Status, output, taskError, &runnerID)
	if err != nil && !completed {
		// The completion was not recorded, so the report is lost and the
		// runner may send it again: this is the server's error.
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !completed {
		writeError(w, http.StatusConflict, "stale epoch, job not owned by this runner, or job already terminal")
		return
	}
	if err != nil {
		// The completion was recorded - the job is terminal and the run's
		// results hold the task's outcome - but the run's resumption
		// failed. There is nothing for the runner to retry: the job is
		// already terminal, and a second report would be answered 409.
		// The run's own state says what happened, so the failure is
		// logged, not returned.
		slog.Error("runner job completion recorded, but the run's resumption failed", "job_id", jobID, "error", redact.Err(err, secrets))
	}
	w.WriteHeader(http.StatusNoContent)
}

// runnerCreateRequest is the body POST /api/runners accepts. Tags are
// optional and assigned only here, by the administrator - a runner never
// declares its own (remote-runner-management design.md).
type runnerCreateRequest struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

// runnerCreateResponse is POST /api/runners' response; Token is shown once.
type runnerCreateResponse struct {
	ID    int      `json:"id"`
	Name  string   `json:"name"`
	Tags  []string `json:"tags"`
	Token string   `json:"token"`
}

// handleCreateRunner implements POST /api/runners (tasks.md 3.5):
// admin-only, returns the bearer token once.
//
// @Summary Register a runner
// @Tags runners
// @Accept json
// @Produce json
// @Param body body runnerCreateRequest true "Runner name and optional tags"
// @Success 201 {object} runnerCreateResponse
// @Failure 400 {object} map[string]string
// @Router /api/runners [post]
func (s *Server) handleCreateRunner(w http.ResponseWriter, r *http.Request) {
	var req runnerCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	tags, err := domain.NormalizeRunnerTags(req.Tags)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.runnerStore == nil {
		writeError(w, http.StatusInternalServerError, "runners not configured")
		return
	}
	id, token, err := s.runnerStore.CreateRunner(req.Name, tags)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tags == nil {
		tags = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(runnerCreateResponse{ID: id, Name: req.Name, Tags: tags, Token: token})
}

// runnerView is one entry of GET /api/runners. It never carries the token
// or its hash.
type runnerView struct {
	domain.Runner
	Status string `json:"status"`
}

// handleListRunners implements GET /api/runners (admin only): every
// unrevoked runner with its tags and liveness - busy, online, offline, or
// never seen (remote-runner-management design.md).
//
// @Summary List registered runners
// @Tags runners
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/runners [get]
func (s *Server) handleListRunners(w http.ResponseWriter, _ *http.Request) {
	if s.runnerStore == nil {
		writeError(w, http.StatusInternalServerError, "runners not configured")
		return
	}
	runners, err := s.runnerStore.ListRunners()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	views := make([]runnerView, len(runners))
	for i, runner := range runners {
		if runner.Tags == nil {
			runner.Tags = []string{}
		}
		views[i] = runnerView{Runner: runner, Status: domain.RunnerStatus(runner, now, s.runnerOfflineAfter())}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": views})
}

// handleRevokeRunner implements DELETE /api/runners/{id} (tasks.md 3.5):
// admin-only.
func (s *Server) handleRevokeRunner(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runner id")
		return
	}
	if s.runnerStore == nil {
		writeError(w, http.StatusInternalServerError, "runners not configured")
		return
	}
	if err := s.runnerStore.RevokeRunner(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "revoked"})
}

// handleRunnerJobArtifacts implements PUT /api/runner-jobs/{id}/artifacts?epoch=N
// (remote-task-artifacts design D3): accepts a gzip tar archive of declared paths,
// epoch- and ownership-fenced, capped by KESTRIX_RUNNER_ARTIFACT_MAX_BYTES (default 50 MiB).
func (s *Server) handleRunnerJobArtifacts(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	runnerID, ok := runnerIDFromContext(r.Context())
	if !ok || s.remoteJobStore == nil {
		writeError(w, http.StatusInternalServerError, "runner jobs not configured")
		return
	}
	epochStr := r.URL.Query().Get("epoch")
	epoch, err := strconv.Atoi(epochStr)
	if err != nil || epoch <= 0 {
		writeError(w, http.StatusBadRequest, "invalid or missing epoch")
		return
	}

	job, err := s.remoteJobStore.GetRemoteJob(jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load remote job")
		return
	}
	if job == nil || job.ClaimEpoch != epoch || domain.IsTerminalJobStatus(job.Status) || job.RunnerID == nil || *job.RunnerID != runnerID {
		writeError(w, http.StatusConflict, "stale epoch, job not owned by this runner, or job already terminal")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("artifact archive exceeds limit of %d bytes", s.runnerArtifactMaxBytes()))
			return
		}
		writeError(w, http.StatusBadRequest, "read artifact body")
		return
	}

	if err := validateArtifactArchive(body, job.Payload.Repos, job.Payload.Task); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.remoteJobStore.PutRemoteArtifact(job.RunID, job.TaskName, body); err != nil {
		writeError(w, http.StatusInternalServerError, "store remote artifact")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validateArtifactArchive validates that body is a valid gzip tar archive and that
// every entry is within the task's declared artifact paths (design D1/D3).
func validateArtifactArchive(body []byte, repos []domain.Repo, task domain.Task) error {
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("invalid gzip archive: %w", err)
	}
	defer gz.Close()

	var repoPath string
	for _, r := range repos {
		if r.Name == task.Execute.Repo {
			repoPath = r.Path
			break
		}
	}

	allowedPrefixes := make([]string, 0, len(task.Artifacts))
	for _, art := range task.Artifacts {
		clean := filepath.ToSlash(filepath.Clean(filepath.Join(repoPath, art)))
		if clean == "." {
			clean = ""
		}
		allowedPrefixes = append(allowedPrefixes, clean)
	}

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}
		name := filepath.ToSlash(filepath.Clean(hdr.Name))
		if name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
			return fmt.Errorf("artifact entry %q is outside declared paths", hdr.Name)
		}
		matched := false
		for _, prefix := range allowedPrefixes {
			if prefix == "" {
				matched = true
				break
			}
			if name == prefix || strings.HasPrefix(name, prefix+"/") {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("artifact entry %q is outside declared paths", hdr.Name)
		}
	}
	return nil
}
