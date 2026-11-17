package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"kestrix/internal/dag"
	"kestrix/internal/domain"
	"kestrix/internal/metrics"
	"kestrix/internal/redact"
)

// defaultCancelGrace is used by Cancel when CancelGrace is unset (zero),
// mirroring the runner package's own default; see design.md D4/D5.
const defaultCancelGrace = 10 * time.Second

// CancelWaitBuffer is added to CancelGrace when Cancel waits for an active
// run's executeRun goroutine to return; see design.md D5. A package
// variable so unit tests aren't forced to wait a real 5 seconds to reach
// the timeout path.
var CancelWaitBuffer = 5 * time.Second

// ErrRunCancelTimedOut identifies a Cancel call whose run did not terminate
// within its grace period; callers use errors.Is to map this distinctly
// (for example to an HTTP 504) without exposing the underlying message,
// which may name internal timing but never a host path or secret.
var ErrRunCancelTimedOut = errors.New("run did not terminate in time")

// activeRun tracks one in-flight run: its cancellation func and the project
// it belongs to, so callers can ask which projects have runs in flight
// without loading run rows from the store (see project-delete-run-guard
// design.md D1).
type activeRun struct {
	cancel  context.CancelFunc
	project string
	// canceledBy is the actor Cancel is canceling this run on behalf of,
	// set just before cancel() runs. Read back by persistCanceledByIfLost
	// when the run's own executeRun/runTaskInternal call - not CancelRun -
	// wins the race to record the canceled transition; see REVIEW.md R9.
	canceledBy string
}

// RunService handles run lifecycle with background execution.
type RunService struct {
	runStore   domain.RunStore
	pipeline   domain.PipelineRunner
	taskRunner domain.TaskRunner
	// remoteJobStore dispatches jobs for a "remote"-mode project instead of
	// executing in-process; see remote-runner-registration design.md D10.
	// Nil (the default, until SetRemoteJobStore is called) means Start,
	// RunTask, and ApproveManualTask never branch into that path.
	remoteJobStore domain.RemoteJobStore
	// runLocks serializes the whole completion path per run ID
	// (remote-runner-registration REVIEW.md R3/R4, remote-run-waits-for-
	// dependents design.md D3): two jobs of one run completing at nearly the
	// same time both record a result, resume the run and finish it, and
	// without this a dependent both racers agree has become eligible could
	// be dispatched twice - and one of them could finish the run before the
	// other one's result is recorded. See remote_dispatch.go's runLocker doc
	// for why an in-process mutex is enough here rather than a DB-level
	// lock, and docs/architecture.md for the single-instance constraint it
	// rests on.
	runLocks *runLocker

	// CancelGrace bounds how long Cancel waits, beyond an extra 5s, for an
	// active run's executeRun goroutine to return after its context is
	// cancelled. Zero selects defaultCancelGrace; see design.md D5.
	CancelGrace time.Duration

	// MaxOutputBytes bounds stored run output; see run-concurrency-and-retention
	// design.md D4. Zero (the default) means unbounded.
	MaxOutputBytes int

	// Metrics records run terminal-status counts and durations; see
	// observability design.md D2. Nil is treated the same as a metrics.NoOp:
	// every call site nil-checks before recording, so a RunService built
	// without one (most tests) behaves exactly as before this field existed.
	Metrics metrics.Recorder

	// slots is a counting semaphore bounding concurrent runs; see
	// run-concurrency-and-retention design.md D1. Nil (the default, set by
	// NewRunService) means unlimited concurrency - SetMaxConcurrentRuns
	// installs a bounded channel pre-filled with one token per slot.
	slots chan struct{}

	// eventHub distributes real-time events to active SSE subscribers.
	eventHub *RunEventHub

	mu     sync.Mutex
	active map[int]activeRun
	done   map[int]chan struct{}

	// allowLocal and defaultMode back CheckLocalAllowed - see
	// safe-production-execution design.md D1. allowLocal mirrors
	// runner.allow_local; defaultMode is the instance's configured
	// runner.mode, used to resolve a project's empty Mode the same way the
	// executor does.
	allowLocal  bool
	defaultMode string
}

// SetLocalExecutionPolicy configures CheckLocalAllowed: allowLocal mirrors
// runner.allow_local, and defaultMode is the instance's runner.mode, used
// to resolve an empty project Mode.
func (s *RunService) SetLocalExecutionPolicy(allowLocal bool, defaultMode string) {
	s.allowLocal = allowLocal
	s.defaultMode = defaultMode
}

// ErrLocalExecutionDisabled identifies an execution attempt rejected because
// runner.allow_local is false and the resolved execution mode is local.
var ErrLocalExecutionDisabled = errors.New(domain.LocalExecutionDisabledMessage())

// CheckLocalAllowed fails a run before any source is acquired when it would
// execute in `local` mode while runner.allow_local is false - see
// safe-production-execution spec.md "Stored local project does not
// execute". mode is normally a stored project's own Mode, already
// populated by the store; an empty mode (e.g. a project lookup that failed
// best-effort) resolves against the instance's configured default the same
// way modeSelectingExecutor does.
func (s *RunService) CheckLocalAllowed(mode string) error {
	if s.allowLocal {
		return nil
	}
	if domain.ResolveMode(mode, s.defaultMode) != "local" {
		return nil
	}
	return ErrLocalExecutionDisabled
}

// NewRunService creates a RunService.
func NewRunService(runStore domain.RunStore, pipeline domain.PipelineRunner, taskRunner domain.TaskRunner) *RunService {
	return &RunService{
		runStore:   runStore,
		pipeline:   pipeline,
		taskRunner: taskRunner,
		active:     make(map[int]activeRun),
		done:       make(map[int]chan struct{}),
		runLocks:   newRunLocker(),
		eventHub:   NewRunEventHub(),
	}
}

// SetEventHub configures the RunEventHub used by RunService.
func (s *RunService) SetEventHub(hub *RunEventHub) {
	s.eventHub = hub
}

// EventHub returns the RunEventHub configured on RunService.
func (s *RunService) EventHub() *RunEventHub {
	return s.eventHub
}

// publishRunState broadcasts runID's overall status and, when status is
// terminal, a completion event - the single place that pairs the two so
// every finishing path agrees on what "terminal" means and on the hub's
// nil-safety, instead of each call site repeating both (see REVIEW.md R9).
func (s *RunService) publishRunState(runID int, status, errMsg string) {
	s.eventHub.BroadcastStatus(runID, status, errMsg)
	if domain.IsTerminalRunStatus(status) {
		s.eventHub.BroadcastComplete(runID, status)
	}
}

// ActiveRunIDsForProject returns the sorted IDs of runs currently tracked
// in active that belong to projectName. Project deletion must be refused
// (409) while the result is non-empty rather than cascading those runs'
// rows from under them.
// ActiveRunIDsForProject's error return covers only the store-backed
// remote-job check added by remote-runner-registration design.md D10: the
// in-memory half above never fails. Callers - handleDeleteProject in
// particular - MUST fail closed (refuse the delete) on a non-nil error
// rather than treating it as "no active runs", which the archived
// project-delete-run-guard change's invariant ("never fail open into
// deletion") already established for the in-memory-only check this extends.
func (s *RunService) ActiveRunIDsForProject(projectName string) ([]int, error) {
	s.mu.Lock()
	ids := make(map[int]struct{})
	for id, entry := range s.active {
		if entry.project == projectName {
			ids[id] = struct{}{}
		}
	}
	s.mu.Unlock()

	if s.remoteJobStore != nil {
		remoteIDs, err := s.remoteJobStore.OutstandingJobRunIDsForProject(projectName)
		if err != nil {
			return nil, fmt.Errorf("check outstanding remote jobs for %q: %w", projectName, err)
		}
		for _, id := range remoteIDs {
			ids[id] = struct{}{}
		}
	}

	out := make([]int, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Ints(out)
	return out, nil
}

// Start creates a run and executes the pipeline in the background.
// Returns the run ID immediately; execution happens asynchronously.
func (s *RunService) Start(ctx context.Context, projectName string, proj *domain.Project, changed []string, opts domain.RunnerOptions) (int, error) {
	if err := s.CheckLocalAllowed(opts.Mode); err != nil {
		return 0, err
	}
	s.cancelSupersededRuns(ctx, projectName, proj, opts)

	if opts.Mode == "remote" && s.remoteJobStore != nil {
		// No in-process execution at all for a remote-mode project: no
		// goroutine, no active/done entry, no concurrency slot - see
		// design.md D10 and tasks.md 2.3/2.4.
		return s.startRemote(projectName, proj, changed, opts)
	}
	if s.pipeline == nil {
		return 0, fmt.Errorf("pipeline runner not configured")
	}
	runID, skippedTasks, err := s.createRun(projectName, changed, opts.IncludeManual, proj, opts.Actor, opts.InitialSources)
	if err != nil {
		return 0, err
	}
	if s.eventHub != nil {
		s.eventHub.BroadcastStatus(runID, "running", "")
		for _, st := range skippedTasks {
			s.eventHub.BroadcastTask(runID, st.Name, st.Status, st.Error)
		}
	}
	startedAt := time.Now()
	slog.Info("run started", "run_id", runID, "project", projectName, "account", opts.Actor)
	output := &bytes.Buffer{}
	redactor := s.prepareRunExecution(runID, output, &opts)

	runCtx, cancel := context.WithCancel(ctx)
	doneCh := make(chan struct{})
	s.mu.Lock()
	s.active[runID] = activeRun{cancel: cancel, project: projectName}
	s.done[runID] = doneCh
	s.mu.Unlock()

	// Decide and record queued-vs-running synchronously, before returning
	// runID, so a caller that immediately queries the run never races the
	// background goroutine's own scheduling; see run-concurrency-and-retention
	// design.md D1/D2. The blocking half (waitAndStart) runs in the goroutine
	// below since it may wait indefinitely. StartTask makes the same
	// synchronous/async split for a single task; acquireSlot composes both
	// halves for the synchronous RunTask path.
	acquired := s.markQueuedIfBusy(runID)

	go func() {
		defer close(doneCh)
		defer cancel()
		defer func() {
			s.mu.Lock()
			delete(s.active, runID)
			delete(s.done, runID)
			s.mu.Unlock()
		}()
		if !acquired && !s.waitAndStart(runCtx, runID) {
			return
		}
		defer s.releaseSlot()
		s.executeRun(runCtx, runID, proj, changed, skippedTasks, opts, output, redactor, startedAt)
	}()
	return runID, nil
}

// recordRunFinished records one run's terminal status and duration on
// s.Metrics, a no-op when Metrics is nil (a RunService built without one,
// as most tests do) - see observability design.md D2.
func (s *RunService) recordRunFinished(status, project string, startedAt time.Time) {
	if s.Metrics == nil {
		return
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	s.Metrics.RunFinished(status, project, time.Since(startedAt))
}

// recordCanceledMetric records the canceled-run metric only when changed is
// true - i.e. this call's CancelRun was the one that actually performed the
// running/queued → canceled transition, per its own atomic status-guarded
// UPDATE. Project and start time are read fresh via GetRun rather than
// pre-checked before CancelRun, since neither changes across cancellation;
// this avoids the check-then-act race a pre-check would reintroduce - see
// REVIEW.md R2.
func (s *RunService) recordCanceledMetric(changed bool, runID int) {
	if !changed || s.Metrics == nil || s.runStore == nil {
		return
	}
	run, err := s.runStore.GetRun(runID)
	if err != nil || run == nil {
		return
	}
	s.recordRunFinished("canceled", run.ProjectName, run.StartedAt)
}

// persistCanceledByIfLost records canceled_by for a run that just
// transitioned to "canceled" via finishRun/runTaskInternal's own store call
// rather than through CancelRun - CancelRun's status-guarded UPDATE (R2) has
// already lost the race and no-ops in that case, so nothing else would ever
// persist who canceled it. A no-op unless Cancel recorded a canceling actor
// on this run's active entry before canceling its context; see REVIEW.md R9.
// Callers only reach this with s.runStore already known non-nil.
func (s *RunService) persistCanceledByIfLost(runID int) {
	s.mu.Lock()
	canceledBy := s.active[runID].canceledBy
	s.mu.Unlock()
	if canceledBy == "" {
		return
	}
	_ = s.runStore.SetCanceledBy(runID, canceledBy)
}

// Cancel stops an active run's executing task before its status is recorded
// as canceled: it cancels the run's context, waits up to CancelGrace plus 5s
// for its executeRun goroutine to return, then marks the run canceled in the
// store. If the wait times out, the run is left running and an error is
// returned instead - a run reported canceled while its process keeps
// executing would be worse than one correctly still reported running; see
// design.md D5. A run with no active entry (already finished, waiting for
// manual approval, or from before a restart) is canceled in the store
// directly, with no termination attempted.
func (s *RunService) Cancel(ctx context.Context, runID int, actor string) error {
	s.mu.Lock()
	entry, hasEntry := s.active[runID]
	doneCh, hasDone := s.done[runID]
	if hasEntry {
		entry.canceledBy = actor
		s.active[runID] = entry
	}
	s.mu.Unlock()

	if !hasEntry || !hasDone {
		if s.runStore == nil {
			return fmt.Errorf("runs not supported")
		}
		// A remote-mode run never registers an active/done entry (design.md
		// D10), so it always takes this branch - no mode-conditional is
		// added here. Marking its remaining pending jobs unclaimable is
		// harmless (a no-op) for a run with no remote_jobs rows at all;
		// this call never waits for a claimed/running job, since no stop
		// signal exists (D8) - see tasks.md 4.3.
		if s.remoteJobStore != nil {
			if err := s.remoteJobStore.CancelJobsForRun(runID); err != nil {
				return fmt.Errorf("cancel remote jobs for run %d: %w", runID, err)
			}
		}
		changed, err := s.runStore.CancelRun(runID, actor)
		if err != nil {
			return err
		}
		slog.Info("run canceled", "run_id", runID, "account", actor)
		s.recordCanceledMetric(changed, runID)
		if changed {
			s.publishRunState(runID, domain.RunStatusCanceled, "")
		}
		return nil
	}

	entry.cancel()
	grace := s.CancelGrace
	if grace <= 0 {
		grace = defaultCancelGrace
	}
	timeout := grace + CancelWaitBuffer
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-doneCh:
		// An active entry only exists once Start has registered it, which
		// requires runStore to create the run in the first place - so
		// runStore is guaranteed non-nil here. doneCh only closes after
		// executeRun/runTaskInternal (and, on the path that actually ran
		// something, its own metric recording) has fully returned - see
		// Start's defer order - so by this point a run that executed has
		// already had its terminal status, including any metric, recorded
		// once. CancelRun's own status-guarded UPDATE is what keeps this
		// from recording a second one: for a run that reached execution,
		// its status here is already terminal (not "running"/"queued"), so
		// CancelRun performs no transition and changed is false; for a run
		// canceled while still queued - which never reached execution, so
		// nothing else ever recorded it - CancelRun does transition it, so
		// this does record. See REVIEW.md R1/R2.
		changed, err := s.runStore.CancelRun(runID, actor)
		if err != nil {
			return err
		}
		slog.Info("run canceled", "run_id", runID, "account", actor)
		s.recordCanceledMetric(changed, runID)
		if changed {
			s.publishRunState(runID, domain.RunStatusCanceled, "")
		}
		return nil
	case <-timer.C:
		return fmt.Errorf("run %d did not terminate within %s: %w", runID, timeout, ErrRunCancelTimedOut)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// toChangedSet converts a slice of changed repo names to a set lookup map.
func toChangedSet(changed []string) map[string]bool {
	changedSet := make(map[string]bool, len(changed))
	for _, c := range changed {
		changedSet[c] = true
	}
	return changedSet
}

// resolvePipelineWithSkipped evaluates every task's trigger condition and partitions tasks
// into triggered DAG nodes and trigger-unsatisfied skipped TaskResult items.
func resolvePipelineWithSkipped(proj domain.Project, changed []string) ([]dag.Node, []domain.TaskResult, error) {
	return dag.New(proj).ResolveWithSkipped(toChangedSet(changed))
}

func (s *RunService) createRun(projectName string, changed []string, includeManual bool, proj *domain.Project, startedBy string, initialSources []domain.AcquiredSource) (int, []domain.TaskResult, error) {
	if proj == nil {
		return 0, nil, fmt.Errorf("project not loaded")
	}
	if s.runStore == nil {
		return 0, nil, fmt.Errorf("run store is not configured")
	}
	_, skippedTasks, err := resolvePipelineWithSkipped(*proj, changed)
	if err != nil {
		return 0, nil, fmt.Errorf("resolve pipeline: %w", err)
	}
	runID, err := s.runStore.CreateRun(projectName, changed, "", includeManual, proj, startedBy, initialSources)
	if err != nil {
		return 0, nil, fmt.Errorf("create run: %w", err)
	}
	skippedByName := make(map[string]domain.TaskResult, len(skippedTasks))
	for _, st := range skippedTasks {
		skippedByName[st.Name] = st
	}
	for _, t := range proj.Tasks {
		if st, isSkipped := skippedByName[t.Name]; isSkipped {
			_ = s.runStore.AppendTaskResult(runID, st)
		} else {
			_ = s.runStore.AppendTaskResult(runID, domain.TaskResult{Name: t.Name, Status: domain.TaskStatusPending})
		}
	}
	return runID, skippedTasks, nil
}

// eventHubTraceWriter broadcasts every chunk written to it as a trace
// event, tagged with whichever task is currently running - set via
// setCurrentTask from OnTaskStart - so the client can attribute live
// output to the right task instead of guessing from task_results (see
// REVIEW.md R6).
//
// It wraps a *boundedWriter rather than a generic io.Writer so it can
// broadcast only the bytes boundedWriter actually kept: boundedWriter.Write
// always reports len(p) consumed to satisfy the writer chain's contract,
// even once output is truncated, so that return value cannot tell this tap
// where the bound was reached. Comparing Written() before and after can
// (see REVIEW.md R3) - without it, SSE subscribers would keep receiving
// output and advancing past MaxOutputBytes after the stored run output has
// stopped growing, diverging from what a reload or polling fallback shows.
type eventHubTraceWriter struct {
	mu          sync.Mutex
	w           *boundedWriter
	hub         *RunEventHub
	runID       int
	offset      int
	currentTask string
}

func (tw *eventHubTraceWriter) setCurrentTask(name string) {
	tw.mu.Lock()
	tw.currentTask = name
	tw.mu.Unlock()
}

func (tw *eventHubTraceWriter) Write(p []byte) (n int, err error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	offset := tw.offset
	taskName := tw.currentTask
	before := tw.w.Written()
	n, err = tw.w.Write(p)
	kept := tw.w.Written() - before
	if kept > 0 {
		chunk := string(p[:kept])
		tw.hub.BroadcastTrace(tw.runID, taskName, chunk, offset)
		tw.offset += kept
	}
	return n, err
}

func (tw *eventHubTraceWriter) Flush() error {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	return tw.w.Flush()
}

// prepareRunExecution wires runID's lifecycle callbacks onto opts and wraps
// its output in a redactingWriter built from opts.ResolvedSecretValues, so
// opts.Stdout/Stderr, FlushRunOutput, and FinishRun all see redacted text
// (see runtime-secrets design.md D5). It returns that redactor so the
// caller can Flush it before the run's final output is persisted.
func (s *RunService) prepareRunExecution(runID int, output *bytes.Buffer, opts *domain.RunnerOptions) *redactingWriter {
	previousSourcesAcquired := opts.OnSourcesAcquired
	bounded := newBoundedWriter(output, s.MaxOutputBytes)
	traceWriter := &eventHubTraceWriter{w: bounded, hub: s.eventHub, runID: runID}
	redactor := newRedactingWriter(traceWriter, opts.ResolvedSecretValues)
	opts.Stdout = redactor
	opts.Stderr = redactor
	opts.OnTaskStart = func(name string) {
		traceWriter.setCurrentTask(name)
		if s.runStore != nil {
			_ = s.runStore.AppendTaskResult(runID, domain.TaskResult{Name: name, Status: "running"})
		}
		if s.eventHub != nil {
			s.eventHub.BroadcastTask(runID, name, "running", "")
		}
	}
	opts.OnTaskComplete = func(tr domain.TaskResult) {
		tr.Output = truncateOutput(redactValues(tr.Output, opts.ResolvedSecretValues), s.MaxOutputBytes)
		if s.runStore != nil {
			if err := s.runStore.AppendTaskResult(runID, tr); err != nil {
				slog.Warn("could not append task result", "run_id", runID, "err", redact.Err(err, opts.ResolvedSecretValues))
			}
			if err := s.runStore.FlushRunOutput(runID, output.String()); err != nil {
				slog.Warn("could not flush run output", "run_id", runID, "err", redact.Err(err, opts.ResolvedSecretValues))
			}
		}
		if s.eventHub != nil {
			s.eventHub.BroadcastTask(runID, tr.Name, tr.Status, tr.Error)
		}
	}
	opts.OnSourcesAcquired = func(sources []domain.AcquiredSource) error {
		allSources := append([]domain.AcquiredSource(nil), opts.InitialSources...)
		allSources = append(allSources, sources...)
		if sourceStore, ok := s.runStore.(domain.RunSourceStore); ok {
			if err := sourceStore.UpdateRunSources(runID, allSources); err != nil {
				return fmt.Errorf("persist run sources: %w", err)
			}
		}
		if opts.SourceSessions != nil {
			if err := opts.SourceSessions.Pin(context.Background(), runID, allSources); err != nil {
				_ = opts.SourceSessions.Release(context.Background(), runID)
				return fmt.Errorf("pin run sources: %w", err)
			}
		}
		if previousSourcesAcquired != nil {
			return previousSourcesAcquired(allSources)
		}
		return nil
	}
	return redactor
}

func (s *RunService) executeRun(ctx context.Context, runID int, proj *domain.Project, changed []string, skippedTasks []domain.TaskResult, opts domain.RunnerOptions, output *bytes.Buffer, redactor *redactingWriter, startedAt time.Time) {
	defer func() {
		if r := recover(); r != nil && s.runStore != nil {
			_ = redactor.Flush()
			panicMsg := fmt.Sprintf("panic: %v", r)
			_ = s.runStore.FinishRun(runID, domain.RunStatusFailed, output.String(), panicMsg, nil)
			s.recordRunFinished(domain.RunStatusFailed, proj.Name, startedAt)
			s.publishRunState(runID, domain.RunStatusFailed, panicMsg)
			if opts.SourceSessions != nil {
				_ = opts.SourceSessions.Release(context.Background(), runID)
			}
		}
	}()

	changedSet := toChangedSet(changed)
	taskResults, err := s.pipeline.Run(ctx, *proj, changedSet, opts)
	// Flush before reading output.String() so a secret value sitting in the
	// redactor's cross-write buffer at run end is redacted before it is
	// persisted - see design.md D5.
	_ = redactor.Flush()
	// FinishRun persists taskResults wholesale, overwriting whatever
	// AppendTaskResult wrote incrementally via OnTaskComplete during the
	// run, so this slice must be redacted independently rather than relying
	// on that earlier pass.
	allResults := mergeSkippedResults(taskResults, skippedTasks)
	allResults = redactTaskResults(allResults, opts.ResolvedSecretValues, s.MaxOutputBytes)
	s.finishRun(runID, proj.Name, output.String(), allResults, err, opts.SourceSessions, startedAt, opts.ResolvedSecretValues)
}

// mergeSkippedResults combines runner results with trigger-skipped tasks, preserving
// runner results on collision (since pipeline runners also return trigger-skipped tasks
// from ResolveWithSkipped when executing).
func mergeSkippedResults(results []domain.TaskResult, skipped []domain.TaskResult) []domain.TaskResult {
	have := make(map[string]bool, len(results))
	for _, tr := range results {
		have[tr.Name] = true
	}
	merged := make([]domain.TaskResult, len(results), len(results)+len(skipped))
	copy(merged, results)
	for _, st := range skipped {
		if !have[st.Name] {
			merged = append(merged, st)
		}
	}
	return merged
}

// redactTaskResults returns a copy of results with each Output field
// redacted (see design.md D5) and bounded to maxOutputBytes (see REVIEW.md
// R1); results itself is left unmodified.
func redactTaskResults(results []domain.TaskResult, values []string, maxOutputBytes int) []domain.TaskResult {
	if len(results) == 0 {
		return results
	}
	out := make([]domain.TaskResult, len(results))
	for i, tr := range results {
		tr.Output = truncateOutput(redactValues(tr.Output, values), maxOutputBytes)
		out[i] = tr
	}
	return out
}

func (s *RunService) finishRun(runID int, projectName string, output string, taskResults []domain.TaskResult, runErr error, sourceSessions domain.SourceSessionManager, startedAt time.Time, secrets []string) {
	status, errMsg := runStatus(taskResults, runErr)
	if s.runStore != nil {
		if err := s.runStore.FinishRun(runID, status, output, errMsg, taskResults); err != nil {
			slog.Warn("could not finish run record", "run_id", runID, "err", redact.Err(err, secrets))
		}
		if status == domain.RunStatusCanceled {
			s.persistCanceledByIfLost(runID)
		}
	}
	s.publishRunState(runID, status, errMsg)
	// runStatus can return "running" (a manual task is still pending) - not
	// a terminal status, so it does not count toward kestrix_runs_total; see
	// observability spec.md "Run outcome counted once".
	if status != "running" {
		s.recordRunFinished(status, projectName, startedAt)
	}
	if sourceSessions != nil && !hasExecutableRunState(taskResults) {
		_ = sourceSessions.Release(context.Background(), runID)
	}
}

func runStatus(taskResults []domain.TaskResult, runErr error) (string, string) {
	return domain.CalculateRunStatus(taskResults, runErr)
}

func hasExecutableRunState(taskResults []domain.TaskResult) bool {
	return domain.HasExecutableRunState(taskResults)
}

// trackActive registers runID in active/done so Cancel terminates the
// run's executing task, returning a derived context and a cleanup func the
// caller must defer. The entry records projectName alongside the cancel func
// so ActiveRunIDsForProject can scope by project without a store round trip.
// Cleanup removes this call's entry (only if a concurrent
// call for the same runID has not already replaced it), cancels the
// context, then closes done — the same order Start's goroutine defers rely
// on. When runStore is nil there is nothing Cancel could record into, so no
// registration happens and cleanup is a no-op; this preserves Cancel's
// invariant that an active entry implies a non-nil runStore.
func (s *RunService) trackActive(ctx context.Context, runID int, projectName string) (context.Context, func()) {
	if s.runStore == nil {
		return ctx, func() {}
	}
	runCtx, cancel := context.WithCancel(ctx)
	doneCh := make(chan struct{})
	s.mu.Lock()
	s.active[runID] = activeRun{cancel: cancel, project: projectName}
	s.done[runID] = doneCh
	s.mu.Unlock()
	return runCtx, func() {
		// Delete only when this call's entry is still current: a second
		// concurrent RunTask/StartTask call for the same runID
		// overwrites these maps, and an unconditional delete here would
		// remove the still-running call's entry so Cancel misses it.
		// Channels are comparable, unlike the CancelFunc.
		s.mu.Lock()
		if cur, ok := s.done[runID]; ok && cur == doneCh {
			delete(s.active, runID)
			delete(s.done, runID)
		}
		s.mu.Unlock()
		cancel()
		close(doneCh)
	}
}

// RunTask executes a single task synchronously and appends the result to an
// existing run; ApproveManualTask runs an approved manual task through it.
// It registers the run in active/done like Start so Cancel terminates the
// task's process (via the runner's process-group wiring) instead of only
// rewriting the run's status in the store; see cancel-kills-task-process
// design.md D1/D2. The single-task re-run route uses StartTask instead.
//
// dispatched is true when opts.Mode is "remote" and this call handed
// taskName to a runner instead of executing it - see design.md D10. A
// caller that finalizes the run from this method's return (as
// finishStartedTask does for StartTask's path) MUST NOT do so when
// dispatched is true: the run stays open for the runner's own completion
// report to finalize instead.
func (s *RunService) RunTask(ctx context.Context, runID int, projectName string, proj *domain.Project, taskName string, opts domain.RunnerOptions) (tr domain.TaskResult, dispatched bool, err error) {
	if proj == nil {
		return domain.TaskResult{}, false, fmt.Errorf("project not loaded")
	}
	if err := s.CheckLocalAllowed(opts.Mode); err != nil {
		return domain.TaskResult{}, false, err
	}
	if opts.Mode == "remote" && s.remoteJobStore != nil {
		return s.dispatchSingleTask(runID, proj, taskName, opts)
	}
	if s.taskRunner == nil {
		return domain.TaskResult{}, false, fmt.Errorf("task runner not configured")
	}
	runCtx, untrack := s.trackActive(ctx, runID, projectName)
	defer untrack()
	if !s.acquireSlot(runCtx, runID) {
		return domain.TaskResult{}, false, fmt.Errorf("run %d canceled while queued", runID)
	}
	defer s.releaseSlot()
	tr, err = s.runTaskInternal(runCtx, runID, projectName, proj, taskName, opts)
	return tr, false, err
}

// StartTask is RunTask's asynchronous counterpart, used by the single-task
// re-run route: it creates the run record (persisting any pinned initial sources),
// tracks runID, decides queued-vs-running synchronously through markQueuedIfBusy -
// the same accepting half Start uses - and returns before the task executes,
// leaving the rest to executeStartedTask in a goroutine. It reports whether the
// run was recorded as queued; see ui-single-task-rerun design.md D1 and
// task-rerun-original-revision D2.
func (s *RunService) StartTask(ctx context.Context, projectName string, proj *domain.Project, taskName string, opts domain.RunnerOptions) (runID int, queued bool, err error) {
	if proj == nil {
		return 0, false, fmt.Errorf("project not loaded")
	}
	if err := s.CheckLocalAllowed(opts.Mode); err != nil {
		return 0, false, err
	}
	if s.runStore == nil {
		return 0, false, fmt.Errorf("run store is not configured")
	}

	// Pin repository refs to the exact revisions recorded in InitialSources if present.
	if len(opts.InitialSources) > 0 {
		revByRepo := make(map[string]string, len(opts.InitialSources))
		for _, src := range opts.InitialSources {
			if src.ResolvedRevision != "" {
				revByRepo[src.RepoName] = src.ResolvedRevision
			}
		}
		if len(revByRepo) > 0 {
			pCopy := *proj
			pCopy.Repos = make([]domain.Repo, len(proj.Repos))
			copy(pCopy.Repos, proj.Repos)
			for i := range pCopy.Repos {
				if rev, ok := revByRepo[pCopy.Repos[i].Name]; ok {
					pCopy.Repos[i].Ref = rev
				}
			}
			proj = &pCopy
		}
	}

	runID, err = s.runStore.CreateRun(projectName, nil, taskName, false, proj, opts.Actor, opts.InitialSources)
	if err != nil {
		return 0, false, fmt.Errorf("create run: %w", err)
	}

	if (opts.Mode != "remote" || s.remoteJobStore == nil) && s.taskRunner == nil {
		taskRunnerErr := fmt.Errorf("task runner not configured")
		if finishErr := s.runStore.FinishRun(runID, "failed", "", taskRunnerErr.Error(), nil); finishErr != nil {
			slog.Warn("could not finish run record", "run_id", runID, "err", redact.Err(finishErr, opts.ResolvedSecretValues))
		}
		return runID, false, taskRunnerErr
	}

	if opts.Mode == "remote" && s.remoteJobStore != nil {
		// Dispatched synchronously, like Start's own remote branch
		// (design.md D10): no active/done entry, no concurrency slot, no
		// goroutine - there is nothing here for executeStartedTask to do.
		_, _, err := s.dispatchSingleTask(runID, proj, taskName, opts)
		if err != nil {
			if finishErr := s.runStore.FinishRun(runID, "failed", "", err.Error(), nil); finishErr != nil {
				slog.Warn("could not finish run record", "run_id", runID, "err", redact.Err(finishErr, opts.ResolvedSecretValues))
			}
			return runID, false, err
		}
		return runID, false, nil
	}

	runCtx, untrack := s.trackActive(ctx, runID, projectName)
	acquired := s.markQueuedIfBusy(runID)
	go s.executeStartedTask(runCtx, untrack, acquired, runID, projectName, proj, taskName, opts)
	return runID, !acquired, nil
}

// executeStartedTask is StartTask's background half: it waits for a slot
// unless one was already acquired, executes the task, and records the run's
// terminal state. A named method rather than a closure so tests can drive
// it directly; see ui-single-task-rerun design.md D2 and Risks.
func (s *RunService) executeStartedTask(ctx context.Context, untrack func(), acquired bool, runID int, projectName string, proj *domain.Project, taskName string, opts domain.RunnerOptions) {
	defer untrack()
	if !acquired && !s.waitAndStart(ctx, runID) {
		// Canceled while queued: record nothing here. Cancel waits for this
		// method to return and then makes the canceled transition itself,
		// with canceled_by and its metric - the same as Start's queued path.
		// runTaskInternal never ran, so neither did its workspace cleanup.
		if opts.WorkspaceCleanup != nil {
			opts.WorkspaceCleanup()
		}
		return
	}
	defer s.releaseSlot()
	tr, err := s.runTaskInternal(ctx, runID, projectName, proj, taskName, opts)
	s.finishStartedTask(runID, tr, err, opts.ResolvedSecretValues)
}

// finishStartedTask persists a started task's verdict as the run's final
// record, deriving status and error through runStatus, as finishRun does.
// Unlike finishRun it never overwrites a run Cancel already recorded
// canceled, and never records a metric or canceled_by: runTaskInternal
// already did both from ApplyTaskResult's status.
func (s *RunService) finishStartedTask(runID int, tr domain.TaskResult, runErr error, secrets []string) {
	if s.runStore == nil {
		return
	}
	if run, _ := s.runStore.GetRun(runID); run != nil && run.Status == "canceled" {
		slog.Info("run: task finished after cancel, keeping canceled status", "run_id", runID)
		return
	}
	status, errMsg := runStatus([]domain.TaskResult{tr}, runErr)
	if err := s.runStore.FinishRun(runID, status, tr.Output, errMsg, []domain.TaskResult{tr}); err != nil {
		slog.Warn("could not finish run record", "run_id", runID, "err", redact.Err(err, secrets))
	}
}

func (s *RunService) runTaskInternal(ctx context.Context, runID int, projectName string, proj *domain.Project, taskName string, opts domain.RunnerOptions) (domain.TaskResult, error) {
	startedAt := time.Now()
	if opts.WorkspaceCleanup != nil {
		defer opts.WorkspaceCleanup()
	}
	var approvedBy string
	var approvedAt *time.Time
	if opts.Actor != "" {
		approvedBy = opts.Actor
		t := time.Now()
		approvedAt = &t
	}
	if s.runStore != nil {
		_, _ = s.runStore.ApplyTaskResult(runID, domain.TaskResult{Name: taskName, Status: "running", ApprovedBy: approvedBy, ApprovedAt: approvedAt}, "")
		s.eventHub.BroadcastTask(runID, taskName, "running", "")
	}
	if !opts.SourcesPrepared {
		previousSourcesAcquired := opts.OnSourcesAcquired
		opts.OnSourcesAcquired = func(sources []domain.AcquiredSource) error {
			allSources := append([]domain.AcquiredSource(nil), opts.InitialSources...)
			allSources = append(allSources, sources...)
			if sourceStore, ok := s.runStore.(domain.RunSourceStore); ok {
				if err := sourceStore.UpdateRunSources(runID, allSources); err != nil {
					return fmt.Errorf("persist run sources: %w", err)
				}
			}
			if opts.SourceSessions != nil {
				if err := opts.SourceSessions.Pin(ctx, runID, allSources); err != nil {
					_ = opts.SourceSessions.Release(context.Background(), runID)
					return fmt.Errorf("pin run sources: %w", err)
				}
			}
			if previousSourcesAcquired != nil {
				return previousSourcesAcquired(allSources)
			}
			return nil
		}
	}

	tr, err := s.taskRunner.RunTask(ctx, *proj, taskName, opts)
	if err != nil && tr.Status == "" {
		tr.Status = "failed"
		tr.Error = err.Error()
	}
	if approvedBy != "" {
		tr.ApprovedBy = approvedBy
		tr.ApprovedAt = approvedAt
	}
	// Redact before persisting, whether this task ran directly or through
	// ApproveManualTask - see design.md D5. Bound after redaction, same
	// ordering the streaming path uses - see REVIEW.md R1.
	tr.Output = truncateOutput(redactValues(tr.Output, opts.ResolvedSecretValues), s.MaxOutputBytes)
	var runStatus string
	if s.runStore != nil {
		runStatus, _ = s.runStore.ApplyTaskResult(runID, tr, tr.Output)
		if runStatus == "canceled" {
			s.persistCanceledByIfLost(runID)
		}
		s.eventHub.BroadcastTask(runID, tr.Name, tr.Status, tr.Error)
		if runStatus != "" {
			s.publishRunState(runID, runStatus, "")
		}
	}
	// ApplyTaskResult's returned status tells us, synchronously and without
	// a separate racy GetRun-based guess, whether this call is the one that
	// finalized the run, including a task canceled mid-flight. This is the
	// only place that records the metric for this run's completion:
	// RunTask/ApproveManualTask never call FinishRun, and StartTask's
	// finishStartedTask calls it only to persist the record, never to record
	// a metric.
	if domain.IsTerminalRunStatus(runStatus) {
		s.recordRunFinished(runStatus, projectName, startedAt)
	}
	if opts.SourceSessions != nil && s.runStore != nil {
		if run, getErr := s.runStore.GetRun(runID); getErr == nil && run != nil && !hasExecutableRunState(run.TaskResults) {
			_ = opts.SourceSessions.Release(context.Background(), runID)
		}
	}
	if err != nil {
		return tr, fmt.Errorf("task %q failed: %w", taskName, err)
	}
	return tr, nil
}

func taskResultsByName(run *domain.Run) map[string]domain.TaskResult {
	results := make(map[string]domain.TaskResult)
	if run == nil {
		return results
	}
	for _, tr := range run.TaskResults {
		results[tr.Name] = tr
	}
	return results
}

func manualTaskContext(run *domain.Run, proj *domain.Project, taskName string) (bool, []string) {
	manualOK := false
	dependsOn := []string{}
	if run != nil && run.PipelineDef != nil {
		for _, t := range run.PipelineDef.Tasks {
			if t.Name == taskName {
				manualOK = t.Manual
				dependsOn = t.DependsOn
				break
			}
		}
	}
	if proj != nil {
		for i := range proj.Tasks {
			if proj.Tasks[i].Name == taskName {
				if proj.Tasks[i].Manual {
					manualOK = true
				}
				if len(dependsOn) == 0 {
					dependsOn = proj.Tasks[i].DependsOn
				}
				break
			}
		}
	}
	return manualOK, dependsOn
}

// ApproveManualTask validates a waiting manual task and executes it.
func (s *RunService) ApproveManualTask(ctx context.Context, runID int, projectName string, proj *domain.Project, taskName string, opts domain.RunnerOptions) (domain.TaskResult, error) {
	if err := s.CheckLocalAllowed(opts.Mode); err != nil {
		return domain.TaskResult{}, err
	}
	if s.runStore == nil {
		return domain.TaskResult{}, fmt.Errorf("runs not supported")
	}
	run, err := s.runStore.GetRun(runID)
	if err != nil {
		return domain.TaskResult{}, fmt.Errorf("get run: %w", err)
	}
	if run == nil || run.ProjectName != projectName {
		return domain.TaskResult{}, fmt.Errorf("run not found")
	}

	latest := taskResultsByName(run)
	cur, ok := latest[taskName]
	if !ok {
		return domain.TaskResult{}, fmt.Errorf("task not in run")
	}
	if !domain.IsWaitingForManualApproval(cur) {
		return domain.TaskResult{}, fmt.Errorf("task %q is not waiting for manual approval (status=%s)", taskName, cur.Status)
	}

	manualOK, dependsOn := manualTaskContext(run, proj, taskName)
	if !manualOK {
		return domain.TaskResult{}, fmt.Errorf("task is not manual")
	}

	var currentRef, currentSHA string
	for _, src := range run.Sources {
		if src.Primary {
			currentRef = src.RequestedRef
			currentSHA = src.ResolvedRevision
			break
		}
	}
	if currentRef == "" && len(run.Sources) > 0 {
		currentRef = run.Sources[0].RequestedRef
		currentSHA = run.Sources[0].ResolvedRevision
	}

	for _, dep := range dependsOn {
		d, exists := latest[dep]
		if exists {
			if d.Status != "passed" {
				return domain.TaskResult{}, fmt.Errorf("dependency %q has not passed", dep)
			}
			continue
		}

		if currentRef == "" || currentSHA == "" {
			return domain.TaskResult{}, fmt.Errorf("dependency %q has not passed", dep)
		}

		status, sha, err := s.runStore.FindLatestTaskStatus(projectName, currentRef, dep)
		if err != nil {
			return domain.TaskResult{}, fmt.Errorf("lookup historical dependency %q: %w", dep, err)
		}
		if status != "passed" || sha != currentSHA {
			return domain.TaskResult{}, fmt.Errorf("dependency %q has not passed", dep)
		}
	}

	slog.Info("manual task approved", "run_id", runID, "project", projectName, "account", opts.Actor, "task", taskName)
	tr, _, err := s.RunTask(ctx, runID, projectName, proj, taskName, opts)
	return tr, err
}
