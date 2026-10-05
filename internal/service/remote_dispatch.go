package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"kestrix/internal/dag"
	"kestrix/internal/domain"
	"kestrix/internal/model"
)

// SetRemoteJobStore wires the port a "remote"-mode project dispatches
// through. Nil (the default) means no RunService method may branch into
// the remote path - Start falls through to its in-process behavior
// unconditionally when this is unset, so existing callers that never wire
// one keep working exactly as before this field existed.
func (s *RunService) SetRemoteJobStore(store domain.RemoteJobStore) {
	s.remoteJobStore = store
}

// runLocker hands out one *sync.Mutex per run ID, so concurrent
// resumeRemoteRun calls for the same run serialize while calls for
// different runs never block each other. An in-process lock is enough to
// close the double-dispatch race REVIEW.md's R4 describes because Phase 1
// has exactly one control-plane process owning remote_jobs (design.md
// Risks: "two runners, one project" scale) - the same assumption
// runnerPollLimiter (internal/web/handler_runner_jobs.go) already makes for
// its own per-runner concurrency bound. Entries are never removed: Phase 1's
// run volume makes that an acceptable, bounded-by-run-count trade against
// the complexity of reference-counted cleanup.
type runLocker struct {
	mu    sync.Mutex
	locks map[int]*sync.Mutex
}

func newRunLocker() *runLocker {
	return &runLocker{locks: make(map[int]*sync.Mutex)}
}

// lock acquires runID's mutex, creating it on first use, and returns a func
// that releases it.
func (r *runLocker) lock(runID int) func() {
	r.mu.Lock()
	m, ok := r.locks[runID]
	if !ok {
		m = &sync.Mutex{}
		r.locks[runID] = m
	}
	r.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// archiveWorkDir packages workDir into an in-memory tar.gz - the source
// archive a remote job's payload carries (design.md D6). An empty
// directory produces a valid, empty-of-entries archive; there is no upper
// size bound in Phase 1 (design.md Risks: acceptable at PoC scale).
func archiveWorkDir(workDir string) ([]byte, error) {
	return archiveWorkDirWithOpener(workDir, os.Open)
}

// archiveWorkDirWithOpener is archiveWorkDir's body, taking the regular-
// file open call as a seam: production always passes os.Open, while a test
// can substitute a function that fails deterministically. That failure is
// otherwise awkward to trigger portably for a real regular file - a
// permission-bit trick doesn't stop root (which a CI job commonly runs as),
// and OS-level tricks that do stop root (resource limits, exotic file
// types) either fail earlier, inside filepath.Walk's own directory reads,
// or crash the tar writer on an unsupported file type.
func archiveWorkDirWithOpener(workDir string, open func(string) (*os.File, error)) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	err := filepath.Walk(workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == workDir {
			return nil
		}
		// Rel cannot fail: Walk only ever calls back with paths it derived
		// by descending from workDir. Readlink and FileInfoHeader cannot
		// fail either, for the same reason - info comes from the Lstat
		// Walk just performed on path, so it describes exactly the file (or
		// symlink) still there to read.
		rel, _ := filepath.Rel(workDir, path)
		if info.IsDir() {
			hdr, _ := tar.FileInfoHeader(info, "")
			hdr.Name = rel + "/"
			return tw.WriteHeader(hdr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, _ := os.Readlink(path)
			hdr, _ := tar.FileInfoHeader(info, link)
			hdr.Name = rel
			return tw.WriteHeader(hdr)
		}
		// A git checkout holds directories, regular files, and symlinks -
		// nothing else. Kestrix's own source is not special-cased. Any
		// WriteHeader failure here (as unreachable as for the two cases
		// above) still surfaces: it would break the tar stream in a way
		// the following io.Copy's own write would report.
		hdr, _ := tar.FileInfoHeader(info, "")
		hdr.Name = rel
		_ = tw.WriteHeader(hdr)
		f, err := open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("archive workdir: %w", err)
	}
	// tw.Close/gz.Close only report a writer-internal inconsistency (for
	// example a header size that didn't match the bytes written, which the
	// code above never produces) - not a condition this function's own
	// inputs can induce, so there is nothing meaningful to test beyond the
	// success path already covered above.
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes(), nil
}

// eligibleForDispatch returns, from nodes (a triggered-only DAG resolution,
// topological order preserved), the tasks that are immediately dispatchable
// given latest - the run's current per-task-name result - along with any
// task that becomes newly "skipped" (dependency failed) as a side effect.
// It applies the same blocked/manual/ordering rules internal/runner's
// taskLoop applies in-process, over persisted results instead of an
// in-memory loop - see design.md Goals ("the one thing that is not reuse")
// and tasks.md 3.6. A task already dispatched, terminal, or still waiting
// on an unresolved (not yet passed/failed/skipped/canceled) dependency is
// left alone; the caller re-evaluates after the next completion.
func eligibleForDispatch(nodes []dag.Node, latest map[string]domain.TaskResult) (eligible []model.Task, newlySkipped []domain.TaskResult) {
	for _, node := range nodes {
		task := node.Task
		_, seeded := latest[task.Name]
		if seeded {
			// A task_result entry only ever appears here once this walker
			// (or startRemote's own first pass) has already decided this
			// task's fate: dispatched (recorded "pending" - awaiting its
			// runner, not "still needs deciding"), or resolved to a
			// terminal status by a completion. Either way, never
			// re-examine it - see the D9/D10 append-only shape this
			// mirrors.
			continue
		}
		if task.Manual {
			// A manual task is dispatched only by its own approval
			// (tasks.md 2.4), never by this walker.
			continue
		}
		blocked := false
		unresolved := false
		for _, dep := range task.DependsOn {
			d, ok := latest[dep]
			if !ok || d.Status == "pending" || d.Status == "running" || d.Status == "claimed" || d.Status == "manual" {
				unresolved = true
				break
			}
			if d.Status == "failed" || d.Status == "canceled" || d.Status == "skipped" {
				blocked = true
				break
			}
		}
		if unresolved {
			continue
		}
		if blocked {
			newlySkipped = append(newlySkipped, domain.TaskResult{Name: task.Name, Status: "skipped", Error: "dependency failed"})
			// Mark it resolved for any sibling that depends on it in this
			// same pass, so a diamond graph's blocked branch propagates in
			// one call rather than needing an extra round trip.
			latest[task.Name] = domain.TaskResult{Name: task.Name, Status: "skipped", Error: "dependency failed"}
			continue
		}
		eligible = append(eligible, task)
		latest[task.Name] = domain.TaskResult{Name: task.Name, Status: "pending"}
	}
	return eligible, newlySkipped
}

// buildRemoteJobPayload constructs the payload a runner receives for task:
// the task definition, its fully-resolved pipeline variables (never a
// control-plane variable - task-execution-isolation - since
// domain.VariableResolver only ever surfaces pipeline-declared values, not
// host environment or KESTRIX_RUNNER_ENV_PASSTHROUGH names), and the source
// archive, plus the run's resolved secret values so the runner can redact
// its own output (remote-runner-fixes design.md D1). It names no execution
// engine or image (design.md D3).
func buildRemoteJobPayload(task model.Task, repos []domain.Repo, defaultImage string, resolver domain.VariableResolver, archive []byte, secretValues []string) domain.RemoteJobPayload {
	var vars map[string]string
	if resolver != nil {
		vars = resolver.Get(task.Execute.Repo, task.Name, "")
	}
	return domain.RemoteJobPayload{Task: task, Repos: sanitizeReposForPayload(repos), DefaultImage: defaultImage, Variables: vars, SecretValues: secretValues, Archive: archive}
}

// sanitizeReposForPayload strips URL and CredentialRef from repos, keeping
// only Name/Path/Kind: the runner needs those to resolve task.Execute.Repo's
// working directory within the extracted archive - it never clones anything
// itself (D6), so a source URL or a credential reference would be dead
// weight at best. Shared by buildRemoteJobPayload and resumeRemoteRun's own
// payload construction, so neither path can forget it.
func sanitizeReposForPayload(repos []domain.Repo) []domain.Repo {
	sanitized := make([]domain.Repo, len(repos))
	for i, r := range repos {
		sanitized[i] = domain.Repo{Name: r.Name, Path: r.Path, Kind: r.Kind}
	}
	return sanitized
}

// startRemote is Start's branch for a "remote"-mode project (design.md
// D10): it creates the run, dispatches a job for every immediately-eligible
// task, and returns - no goroutine, no active/done entry, no concurrency
// slot (tasks.md 2.3). Dependents are dispatched later by the completion
// path's resumption walker (tasks.md 3.6), not here.
// startRemote's only caller (Start) has already checked s.remoteJobStore is
// non-nil.
func (s *RunService) startRemote(projectName string, proj *model.Project, changed []string, opts domain.RunnerOptions) (int, error) {
	if opts.WorkspaceCleanup != nil {
		// The archive below copies everything a job needs out of the live
		// checkout; nothing in this function touches opts.WorkDir again
		// after that, so the checkout can be released the same as the
		// in-process path releases it once execution no longer needs it.
		defer opts.WorkspaceCleanup()
	}
	changedSet := make(map[string]bool, len(changed))
	for _, c := range changed {
		changedSet[c] = true
	}
	nodes, err := dag.New(*proj).Resolve(changedSet)
	if err != nil {
		return 0, fmt.Errorf("resolve pipeline: %w", err)
	}

	runID, err := s.runStore.CreateRun(projectName, changed, "", opts.IncludeManual, proj, opts.Actor)
	if err != nil {
		return 0, fmt.Errorf("create run: %w", err)
	}
	slog.Info("run started", "run_id", runID, "project", projectName, "account", opts.Actor, "mode", "remote")

	latest := make(map[string]domain.TaskResult, len(nodes))
	eligible, _ := eligibleForDispatch(nodes, latest)

	var archive []byte
	if len(nodes) > 0 {
		archive, err = archiveWorkDir(opts.WorkDir)
		if err != nil {
			return runID, fmt.Errorf("build source archive: %w", err)
		}
		// Persist the archive and every triggered task's resolved
		// variables once, so tasks.md 3.6's resumption walker can dispatch
		// a dependent task later without re-acquiring source or
		// re-resolving secrets - see design.md D6/D7.
		perTaskVars := make(map[string]map[string]string, len(nodes))
		for _, node := range nodes {
			var vars map[string]string
			if opts.Variables != nil {
				vars = opts.Variables.Get(node.Task.Execute.Repo, node.Task.Name, "")
			}
			perTaskVars[node.Task.Name] = vars
		}
		if err := s.remoteJobStore.SetRemoteRunSnapshot(runID, archive, perTaskVars, opts.ResolvedSecretValues); err != nil {
			return runID, fmt.Errorf("persist remote run snapshot: %w", err)
		}
	}
	for _, task := range eligible {
		_ = s.runStore.AppendTaskResult(runID, domain.TaskResult{Name: task.Name, Status: "pending"})
		payload := buildRemoteJobPayload(task, proj.Repos, proj.DefaultImage, opts.Variables, archive, opts.ResolvedSecretValues)
		if _, err := s.remoteJobStore.CreateRemoteJob(runID, task.Name, payload); err != nil {
			return runID, fmt.Errorf("dispatch task %q: %w", task.Name, err)
		}
	}
	return runID, nil
}

// CompleteRemoteJob is the RunService-level completion path
// PUT /api/runner-jobs/{id}/complete is a thin adapter into (tasks.md 3.4):
// it rejects the report as a no-op when the store's epoch/ownership/
// terminal-run fencing (domain.RemoteJobStore.CompleteJob) says so - a
// caller may only complete a job it holds the current claim on, per
// callerRunnerID - and otherwise triggers the dependency-graph resumption
// walker (tasks.md 3.6) for any task that just became eligible.
func (s *RunService) CompleteRemoteJob(jobID, epoch int, status, output, taskError string, callerRunnerID *int) (bool, error) {
	job, err := s.remoteJobStore.GetRemoteJob(jobID)
	if err != nil {
		return false, fmt.Errorf("get remote job %d: %w", jobID, err)
	}
	if job == nil {
		return false, nil
	}
	ok, err := s.remoteJobStore.CompleteJob(jobID, epoch, status, output, taskError, callerRunnerID, s.MaxOutputBytes)
	if err != nil {
		return false, fmt.Errorf("complete remote job %d: %w", jobID, err)
	}
	if !ok {
		return false, nil
	}
	if err := s.resumeRemoteRun(job.RunID); err != nil {
		return true, fmt.Errorf("resume run %d after job %d: %w", job.RunID, jobID, err)
	}
	return true, nil
}

// resumeRemoteRun is tasks.md 3.6: given runID's persisted task_results and
// its pipeline snapshot, it computes which not-yet-dispatched tasks have
// become eligible - applying the same blocked/manual/ordering rules
// internal/runner's taskLoop applies in-process (eligibleForDispatch) - and
// dispatches a job for each, reusing the archive and resolved variables
// startRemote persisted once at run creation (design.md D6/D7). A task
// newly blocked by a failed dependency is recorded "skipped" via the same
// path CancelRun uses for a blocked dependent. Serialized per run via
// resumeLocks (REVIEW.md R4): two jobs of one run completing at nearly the
// same time both call this, and without serializing, both could reach the
// same eligibility conclusion about a shared dependent and dispatch it
// twice.
func (s *RunService) resumeRemoteRun(runID int) error {
	unlock := s.resumeLocks.lock(runID)
	defer unlock()

	run, err := s.runStore.GetRun(runID)
	if err != nil {
		return fmt.Errorf("get run %d: %w", runID, err)
	}
	if run == nil || run.PipelineDef == nil {
		return nil
	}
	changedSet := make(map[string]bool, len(run.Changed))
	for _, c := range run.Changed {
		changedSet[c] = true
	}
	nodes, err := dag.New(*run.PipelineDef).Resolve(changedSet)
	if err != nil {
		return fmt.Errorf("resolve pipeline: %w", err)
	}

	latest := taskResultsByName(run)
	eligible, newlySkipped := eligibleForDispatch(nodes, latest)
	if len(eligible) == 0 && len(newlySkipped) == 0 {
		// Nothing to dispatch or skip, so skip the snapshot too: it holds the
		// run's whole source archive, and most completions unblock nothing.
		return nil
	}
	archive, variables, secretValues, err := s.remoteJobStore.GetRemoteRunSnapshot(runID)
	if err != nil {
		return fmt.Errorf("load remote run snapshot: %w", err)
	}
	if archive == nil {
		// No snapshot means a single-task re-run (dispatchSingleTask stores
		// none): every dispatch below needs the archive and variables, and
		// what was computed above would dispatch or skip tasks nobody asked
		// for. Return before the first write - remote-run-ux design.md D2.
		return nil
	}
	for _, skip := range newlySkipped {
		if _, err := s.runStore.ApplyTaskResult(runID, skip, ""); err != nil {
			return fmt.Errorf("record blocked task %q: %w", skip.Name, err)
		}
	}
	for _, task := range eligible {
		_ = s.runStore.AppendTaskResult(runID, domain.TaskResult{Name: task.Name, Status: "pending"})
		payload := domain.RemoteJobPayload{
			Task: task, Repos: sanitizeReposForPayload(run.PipelineDef.Repos), DefaultImage: run.PipelineDef.DefaultImage,
			Variables: variables[task.Name], SecretValues: secretValues, Archive: archive,
		}
		if _, err := s.remoteJobStore.CreateRemoteJob(runID, task.Name, payload); err != nil {
			return fmt.Errorf("dispatch task %q: %w", task.Name, err)
		}
	}
	return nil
}

// ReclaimStaleRemoteJobs is silent-runner recovery's periodic check
// (tasks.md 4.1/4.2): it returns to "pending" every "claimed"/"running" job
// last updated more than timeout ago, or fails it after maxReclaims
// attempts, naming reason - see domain.RemoteJobStore.ReclaimStaleJobs for
// the terminal-run exclusion and epoch-fencing this builds on. A no-op
// (0, 0, nil) when no remoteJobStore is configured, so a caller can invoke
// this unconditionally at startup without checking first.
func (s *RunService) ReclaimStaleRemoteJobs(timeout time.Duration, maxReclaims int, reason string) (int, int, error) {
	if s.remoteJobStore == nil {
		return 0, 0, nil
	}
	return s.remoteJobStore.ReclaimStaleJobs(time.Now().Add(-timeout), maxReclaims, reason)
}

// DeleteTerminalRemoteSecrets deletes the encrypted values of finished
// remote jobs and runs (remote-runner-fixes design.md D2); the reclaim loop
// calls it on every tick. A no-op without a remoteJobStore.
func (s *RunService) DeleteTerminalRemoteSecrets() error {
	if s.remoteJobStore == nil {
		return nil
	}
	return s.remoteJobStore.DeleteTerminalRemoteSecrets()
}

// dispatchSingleTask is RunTask's and StartTask's shared remote-mode branch
// (design.md D10): instead of executing taskName in-process, it builds a
// fresh source archive from opts.WorkDir, dispatches one remote_jobs row,
// and releases the workspace - the same shape startRemote uses for a whole
// run's initial dispatch, narrowed to one task. It registers no
// active/done entry and acquires no concurrency slot: the caller must not
// call trackActive/acquireSlot/markQueuedIfBusy for this path at all.
func (s *RunService) dispatchSingleTask(runID int, proj *model.Project, taskName string, opts domain.RunnerOptions) (domain.TaskResult, bool, error) {
	if opts.WorkspaceCleanup != nil {
		defer opts.WorkspaceCleanup()
	}
	var task *model.Task
	for i := range proj.Tasks {
		if proj.Tasks[i].Name == taskName {
			task = &proj.Tasks[i]
			break
		}
	}
	if task == nil {
		return domain.TaskResult{}, false, fmt.Errorf("task %q not found", taskName)
	}
	archive, err := archiveWorkDir(opts.WorkDir)
	if err != nil {
		return domain.TaskResult{}, false, fmt.Errorf("build source archive: %w", err)
	}
	payload := buildRemoteJobPayload(*task, proj.Repos, proj.DefaultImage, opts.Variables, archive, opts.ResolvedSecretValues)
	if _, err := s.remoteJobStore.CreateRemoteJob(runID, taskName, payload); err != nil {
		return domain.TaskResult{}, false, fmt.Errorf("dispatch task %q: %w", taskName, err)
	}
	tr := domain.TaskResult{Name: taskName, Status: "pending"}
	if s.runStore != nil {
		_, _ = s.runStore.ApplyTaskResult(runID, tr, "")
	}
	return tr, true, nil
}
