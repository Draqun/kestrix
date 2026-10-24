package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
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

// runLocker hands out one *sync.Mutex per run ID, so two completions of one
// run serialize while completions of different runs never block each other.
// CompleteRemoteJob takes the lock and holds it across the whole completion
// path: the result write, the walker that dispatches what the completion
// unblocked, and the finishing of the run.
//
// An in-process lock orders those completions only while one control-plane
// process owns remote_jobs. A second process would reach the same
// eligibility conclusion about a shared dependent and dispatch it twice;
// the database's unique index rejects the second job, and the walker treats
// that rejection as a no-op - the task stays dispatched and the run keeps
// going. The single-process premise is the constraint documented in
// docs/architecture.md ("Single-instance constraint").
//
// Entries are never removed: Phase 1's run volume makes that an acceptable,
// bounded-by-run-count trade against the complexity of reference-counted
// cleanup.
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
// task that becomes newly "skipped" (dependency failed) as a side effect,
// and any manual gates to record (design D1-D3).
// It applies the same blocked/manual/ordering rules internal/runner's
// taskLoop applies in-process, over persisted results instead of an
// in-memory loop - see design.md Goals ("the one thing that is not reuse")
// and tasks.md 3.6. A task already dispatched, terminal, or still waiting
// on an unresolved (not yet passed/failed/skipped/canceled) dependency is
// left alone; the caller re-evaluates after the next completion.
func eligibleForDispatch(nodes []dag.Node, latest map[string]domain.TaskResult, includeManual bool) (eligible []model.Task, newlySkipped []domain.TaskResult, gates []domain.TaskResult) {
	nodeSet := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		nodeSet[node.Task.Name] = true
	}
	for _, node := range nodes {
		task := node.Task
		cur, seeded := latest[task.Name]
		if seeded && (!task.Manual || includeManual || cur.Status != "manual") {
			// A task_result entry only ever appears here once this walker
			// (or startRemote's own first pass) has already decided this
			// task's fate: dispatched (recorded "pending" - awaiting its
			// runner, not "still needs deciding"), or resolved to a
			// terminal status by a completion. Either way, never
			// re-examine it - see the D9/D10 append-only shape this
			// mirrors. The one exception is a waiting "manual" gate, which
			// is re-examined for a failed dependency (design D2).
			continue
		}
		if task.Manual && !includeManual {
			if !seeded {
				gates = append(gates, domain.TaskResult{Name: task.Name, Status: "manual"})
				cur = domain.TaskResult{Name: task.Name, Status: "manual"}
				latest[task.Name] = cur
			}
			// A manual gate is never dispatched by this walker (design D1).
			// Check if any inside dependency failed, was canceled or was skipped (design D2).
			blocked := false
			for _, dep := range task.DependsOn {
				if !nodeSet[dep] {
					continue
				}
				d, ok := latest[dep]
				if !ok {
					continue
				}
				if d.Status == "failed" || d.Status == "canceled" || d.Status == "skipped" {
					blocked = true
					break
				}
			}
			if blocked {
				newlySkipped = append(newlySkipped, domain.TaskResult{Name: task.Name, Status: "skipped", Error: "dependency failed"})
				latest[task.Name] = domain.TaskResult{Name: task.Name, Status: "skipped", Error: "dependency failed"}
			}
			// With a pending or passed dependency, it stays manual (already in latest).
			continue
		}
		blocked := false
		unresolved := false
		for _, dep := range task.DependsOn {
			if !nodeSet[dep] {
				// A dependency outside the run (its trigger was not
				// satisfied) is ignored: it is not part of this run and
				// holds nothing - remote-run-start-never-hangs design.md D1.
				continue
			}
			d, ok := latest[dep]
			if !ok || d.Status == "pending" || d.Status == "running" || d.Status == "claimed" || d.Status == "manual" {
				// The dependency is not decided yet: the task waits,
				// whatever its other dependencies say - the decision
				// belongs to the pass that resolves the last one.
				unresolved = true
				continue
			}
			if d.Status == "failed" || d.Status == "canceled" || d.Status == "skipped" {
				blocked = true
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
	return eligible, newlySkipped, gates
}

// buildRemoteJobPayload constructs the payload a runner receives for task:
// the task definition, its fully-resolved pipeline variables (never a
// control-plane variable - task-execution-isolation - since
// domain.VariableResolver only ever surfaces pipeline-declared values, not
// host environment or KESTRIX_RUNNER_ENV_PASSTHROUGH names), and the source
// archive, plus the run's resolved secret values so the runner can redact
// its own output (remote-runner-fixes design.md D1), and passed ancestors'
// artifacts in dependency order (remote-task-artifacts design.md D4).
func buildRemoteJobPayload(task model.Task, repos []domain.Repo, defaultImage string, resolver domain.VariableResolver, archive []byte, secretValues []string, artifacts ...[][]byte) domain.RemoteJobPayload {
	var vars map[string]string
	if resolver != nil {
		vars = resolver.Get(task.Execute.Repo, task.Name, "")
	}
	var arts [][]byte
	if len(artifacts) > 0 {
		arts = artifacts[0]
	}
	return domain.RemoteJobPayload{
		Task:         task,
		Repos:        sanitizeReposForPayload(repos),
		DefaultImage: defaultImage,
		Variables:    vars,
		SecretValues: secretValues,
		Archive:      archive,
		Artifacts:    arts,
	}
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

	if len(opts.InitialSources) > 0 {
		if sourceStore, ok := s.runStore.(domain.RunSourceStore); ok {
			if err := sourceStore.UpdateRunSources(runID, opts.InitialSources); err != nil {
				return runID, fmt.Errorf("persist run sources: %w", err)
			}
		}
	}

	latest := make(map[string]domain.TaskResult, len(nodes))
	eligible, _, gates := eligibleForDispatch(nodes, latest, opts.IncludeManual)

	if len(nodes) == 0 {
		// Nothing triggered: finish the run passed at once -
		// remote-run-start-never-hangs design.md D2.
		if err := s.runStore.FinishRun(runID, "passed", "", "", nil); err != nil {
			return runID, fmt.Errorf("finish run %d: %w", runID, err)
		}
		return runID, nil
	}

	archive, err := archiveWorkDir(opts.WorkDir)
	if err != nil {
		archiveErr := fmt.Errorf("build source archive: %w", err)
		redactedErr := redactValues(archiveErr.Error(), opts.ResolvedSecretValues)
		_ = s.runStore.FinishRun(runID, "failed", "", redactedErr, nil)
		return runID, archiveErr
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
		snapshotErr := fmt.Errorf("persist remote run snapshot: %w", err)
		redactedErr := redactValues(snapshotErr.Error(), opts.ResolvedSecretValues)
		_ = s.runStore.FinishRun(runID, "failed", "", redactedErr, nil)
		return runID, snapshotErr
	}

	for _, gate := range gates {
		_ = s.runStore.AppendTaskResult(runID, gate)
	}

	var dispatchErrs []error
	for _, task := range eligible {
		_ = s.runStore.AppendTaskResult(runID, domain.TaskResult{Name: task.Name, Status: "pending"})
		payload := buildRemoteJobPayload(task, proj.Repos, proj.DefaultImage, opts.Variables, archive, opts.ResolvedSecretValues)
		if _, err := s.remoteJobStore.CreateRemoteJob(runID, task.Name, payload); err != nil {
			dispatchErrs = append(dispatchErrs, s.recordDispatchFailure(runID, task.Name, fmt.Errorf("dispatch task %q: %w", task.Name, err), opts.ResolvedSecretValues))
			latest[task.Name] = domain.TaskResult{Name: task.Name, Status: "failed"}
		}
	}
	if len(dispatchErrs) > 0 {
		_, newlySkipped, _ := eligibleForDispatch(nodes, latest, opts.IncludeManual)
		for _, skip := range newlySkipped {
			_ = s.runStore.AppendTaskResult(runID, skip)
		}
		_ = s.finishRunWithRetry(runID)
		return runID, fmt.Errorf("dispatch failed: %w", errors.Join(dispatchErrs...))
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
//
// The run's lock covers the whole path: the result write, the walker and
// the finishing of the run. Two jobs of one run completing at the same
// moment are then handled one after the other, so neither can finish the
// run on a view that does not yet hold the other's result -
// remote-run-waits-for-dependents design.md D3.
//
// The store's completion does not finish the run: it only records the
// result, the walker decides what that result unblocked, and only then does
// this function finish the run, with the status its results imply - a run
// whose dependent has not been dispatched yet stays "running"
// (remote-run-waits-for-dependents design.md D1/D2).
func (s *RunService) CompleteRemoteJob(jobID, epoch int, status, output, taskError string, callerRunnerID *int) (bool, error) {
	job, err := s.remoteJobStore.GetRemoteJob(jobID)
	if err != nil {
		return false, fmt.Errorf("get remote job %d: %w", jobID, err)
	}
	if job == nil {
		return false, nil
	}
	unlock := s.runLocks.lock(job.RunID)
	defer unlock()
	ok, err := s.remoteJobStore.CompleteJob(jobID, epoch, status, output, taskError, callerRunnerID, s.MaxOutputBytes)
	if err != nil {
		return false, fmt.Errorf("complete remote job %d: %w", jobID, err)
	}
	if !ok {
		return false, nil
	}
	decided, resumeErr := s.resumeRemoteRun(job.RunID)
	if resumeErr != nil && !decided {
		// The walker failed before it knew what was due, so the run
		// keeps running: finishing it here would hide the tasks that
		// were never dispatched -
		// remote-run-waits-for-dependents design.md D5.
		return true, fmt.Errorf("resume run %d after job %d: %w", job.RunID, jobID, resumeErr)
	}
	// The batch was decided - every due task has a job or a recorded
	// failure - so the run takes the status its results imply, and a
	// dispatch failure travels on top of it -
	// remote-run-waits-for-dependents design.md D5.
	if err := s.finishRunWithRetry(job.RunID); err != nil {
		if resumeErr != nil {
			return true, errors.Join(fmt.Errorf("finish run %d after job %d: %w", job.RunID, jobID, err), resumeErr)
		}
		return true, fmt.Errorf("finish run %d after job %d: %w", job.RunID, jobID, err)
	}
	if resumeErr != nil {
		return true, fmt.Errorf("resume run %d after job %d: %w", job.RunID, jobID, resumeErr)
	}
	return true, nil
}

// finishRunAttempts bounds how often the run's finishing write is retried.
const finishRunAttempts = 3

// finishRunWithRetry finishes the run, retrying a failed write. The completion that
// led here is already recorded, so a lost finishing write would leave the
// run "running" with every task terminal and nothing left that would ever
// finish it - only the startup sweep, which runs once, at process start -
// remote-run-waits-for-dependents design.md D5. A write that keeps failing
// leaves the run running until that sweep marks it failed.
func (s *RunService) finishRunWithRetry(runID int) error {
	var err error
	for attempt := 0; attempt < finishRunAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 50 * time.Millisecond)
		}
		if err = s.runStore.FinishRunIfDone(runID); err == nil {
			return nil
		}
	}
	return err
}

// resumeRemoteRun is tasks.md 3.6: given runID's persisted task_results and
// its pipeline snapshot, it computes which not-yet-dispatched tasks have
// become eligible - applying the same blocked/manual/ordering rules
// internal/runner's taskLoop applies in-process (eligibleForDispatch) - and
// dispatches a job for each, reusing the archive and resolved variables
// startRemote persisted once at run creation (design.md D6/D7). A task
// newly blocked by a failed dependency is recorded "skipped" by the walker
// itself, through AppendTaskResult, with the error "dependency failed" -
// not through CancelRun, which writes the skipped results of a canceled run
// itself, with "dependency canceled". The caller holds the run's lock
// (remote-run-waits-for-dependents design.md D3), which keeps two jobs of
// one run completing at the same moment from reaching the same eligibility
// conclusion about a shared dependent and dispatching it twice.
//
// decided says whether the walker got as far as knowing what was due:
// every due task then has a job or a recorded failure, so the run's status
// can be decided. A false with an error means the walker stopped earlier,
// and the run must stay running (design.md D5).
func (s *RunService) resumeRemoteRun(runID int) (decided bool, err error) {
	run, err := s.runStore.GetRun(runID)
	if err != nil {
		return false, fmt.Errorf("get run %d: %w", runID, err)
	}
	if run == nil || run.PipelineDef == nil {
		return true, nil
	}
	if domain.IsTerminalRunStatus(run.Status) {
		// The run was canceled (or otherwise finished) before this walker
		// ran: it stays that way, and nothing is dispatched for it -
		// remote-run-waits-for-dependents design.md D4.
		return true, nil
	}
	changedSet := make(map[string]bool, len(run.Changed))
	for _, c := range run.Changed {
		changedSet[c] = true
	}
	nodes, err := dag.New(*run.PipelineDef).Resolve(changedSet)
	if err != nil {
		return false, fmt.Errorf("resolve pipeline: %w", err)
	}

	latest := taskResultsByName(run)
	eligible, newlySkipped, gates := eligibleForDispatch(nodes, latest, run.IncludeManual)
	if len(eligible) == 0 && len(newlySkipped) == 0 && len(gates) == 0 {
		// Nothing to dispatch, skip, or record as gate, so skip the snapshot too: it holds the
		// run's whole source archive, and most completions unblock nothing.
		return true, nil
	}
	archive, variables, secretValues, err := s.remoteJobStore.GetRemoteRunSnapshot(runID)
	if err != nil {
		return false, fmt.Errorf("load remote run snapshot: %w", err)
	}
	if archive == nil {
		// No snapshot means a single-task re-run (dispatchSingleTask stores
		// none): every dispatch below needs the archive and variables, and
		// what was computed above would dispatch or skip tasks nobody asked
		// for. Return before the first write - remote-run-ux design.md D2.
		return true, nil
	}
	for _, gate := range gates {
		if err := s.runStore.AppendTaskResult(runID, gate); err != nil {
			return false, fmt.Errorf("record gate %q: %w", gate.Name, err)
		}
	}
	for _, skip := range newlySkipped {
		// AppendTaskResult records the result and leaves the run's status
		// alone: the walker never decides the run's status. The run takes it
		// once, after the walker, through FinishRunIfDone - see
		// remote-run-waits-for-dependents design.md D1.
		if err := s.runStore.AppendTaskResult(runID, skip); err != nil {
			return false, fmt.Errorf("record blocked task %q: %w", skip.Name, err)
		}
	}
	// A dispatch failure does not abandon the batch: every task that became
	// due gets either a job or a recorded failure, and the run's status is
	// decided once, after the batch -
	// remote-run-waits-for-dependents design.md D5.
	passedTasks := make(map[string]bool)
	for name, tr := range latest {
		if tr.Status == "passed" {
			passedTasks[name] = true
		}
	}
	var dispatchErrs []error
	for _, task := range eligible {
		if err := s.runStore.AppendTaskResult(runID, domain.TaskResult{Name: task.Name, Status: "pending"}); err != nil {
			// The "pending" record is what tells the next walker pass that
			// the task is decided; without it the task would be dispatched
			// a second time - so the task is recorded failed instead of
			// being dispatched -
			// remote-run-waits-for-dependents design.md D5.
			dispatchErrs = append(dispatchErrs, s.recordDispatchFailure(runID, task.Name, fmt.Errorf("record pending task %q: %w", task.Name, err), secretValues))
			latest[task.Name] = domain.TaskResult{Name: task.Name, Status: "failed"}
			continue
		}
		ancestors := orderedPassedAncestors(*run.PipelineDef, task, passedTasks)
		var artifacts [][]byte
		if len(ancestors) > 0 {
			artMap, err := s.remoteJobStore.ListRemoteArtifactsByTasks(runID, ancestors)
			if err != nil {
				return false, fmt.Errorf("list ancestors artifacts for task %q: %w", task.Name, err)
			}
			for _, anc := range ancestors {
				if data, ok := artMap[anc]; ok && len(data) > 0 {
					artifacts = append(artifacts, data)
				}
			}
		}
		payload := buildRemoteJobPayload(task, run.PipelineDef.Repos, run.PipelineDef.DefaultImage, nil, archive, secretValues, artifacts)
		payload.Variables = variables[task.Name]
		if _, err := s.remoteJobStore.CreateRemoteJob(runID, task.Name, payload); err != nil {
			if errors.Is(err, domain.ErrRemoteJobExists) {
				// The task already has an active job: it is dispatched,
				// not failed, and the run keeps going.
				continue
			}
			// The task became due and its job cannot be created: record the
			// task failed, naming the failure, so the run ends failed
			// instead of staying running with a task that has no job -
			// remote-run-waits-for-dependents design.md D5.
			dispatchErrs = append(dispatchErrs, s.recordDispatchFailure(runID, task.Name, fmt.Errorf("dispatch task %q: %w", task.Name, err), secretValues))
			latest[task.Name] = domain.TaskResult{Name: task.Name, Status: "failed"}
			continue
		}
	}
	if len(dispatchErrs) > 0 {
		// A task that could not be dispatched is failed, so its own
		// dependents are blocked: record them skipped, so the run's
		// results name every task that will not run. This pass can only
		// skip: every task the first pass made eligible was seeded
		// (dispatched "pending" or recorded "failed"), so none of them is
		// eligible again.
		_, newlyBlocked, _ := eligibleForDispatch(nodes, latest, run.IncludeManual)
		for _, skip := range newlyBlocked {
			if err := s.runStore.AppendTaskResult(runID, skip); err != nil {
				dispatchErrs = append(dispatchErrs, fmt.Errorf("record blocked task %q: %w", skip.Name, err))
			}
		}
		return true, fmt.Errorf("dispatch failed: %w", errors.Join(dispatchErrs...))
	}
	return true, nil
}

// recordDispatchFailure records the task failed, naming why (redacting any
// secret values), so the run ends failed instead of staying running with a
// task that has no job - it returns the failure it recorded, joined with the
// write error when the failure could not be recorded at all.
func (s *RunService) recordDispatchFailure(runID int, taskName string, cause error, secretValues []string) error {
	errMsg := cause.Error()
	if len(secretValues) > 0 {
		errMsg = redactValues(errMsg, secretValues)
	}
	if err := s.runStore.AppendTaskResult(runID, domain.TaskResult{Name: taskName, Status: "failed", Error: errMsg}); err != nil {
		return errors.Join(cause, fmt.Errorf("record failed task %q: %w", taskName, err))
	}
	return cause
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

// orderedPassedAncestors computes the passed transitive dependencies of task,
// ordered topologically with ties broken by declaration order in proj.Tasks
// (remote-task-artifacts design D4).
func orderedPassedAncestors(proj model.Project, task model.Task, passed map[string]bool) []string {
	allTasksByName := make(map[string]model.Task, len(proj.Tasks))
	for _, t := range proj.Tasks {
		allTasksByName[t.Name] = t
	}

	ancestorSet := make(map[string]bool)
	var collectAncestors func(t model.Task)
	collectAncestors = func(t model.Task) {
		for _, depName := range t.DependsOn {
			if !ancestorSet[depName] {
				ancestorSet[depName] = true
				if depTask, ok := allTasksByName[depName]; ok {
					collectAncestors(depTask)
				}
			}
		}
	}
	collectAncestors(task)

	passedAncestors := make(map[string]bool)
	for name := range ancestorSet {
		if passed[name] {
			passedAncestors[name] = true
		}
	}
	if len(passedAncestors) == 0 {
		return nil
	}

	var topo []string
	visited := make(map[string]bool, len(proj.Tasks))
	var visit func(name string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		t, ok := allTasksByName[name]
		if !ok {
			return
		}
		for _, dep := range t.DependsOn {
			visit(dep)
		}
		visited[name] = true
		topo = append(topo, name)
	}
	for _, t := range proj.Tasks {
		visit(t.Name)
	}

	var result []string
	for _, name := range topo {
		if passedAncestors[name] {
			result = append(result, name)
		}
	}
	return result
}
