package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/redact"
	"kestrix/internal/service"

	"kestrix/internal/web/pagination"

	"github.com/go-chi/chi/v5"
)

// handleRunProject executes a registered project's selected tasks from one
// immutable remote source session.
func (s *Server) handleRunProject(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	p, err := s.projectStore.Get(name)
	if err != nil || p == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	runService := s.runService
	if runService == nil {
		writeError(w, http.StatusInternalServerError, "run service not configured")
		return
	}
	if err := runService.CheckLocalAllowed(p.Mode); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		Changed       []string `json:"changed"`
		IncludeManual bool     `json:"include_manual"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	proj, primary, err := s.loadProjectPipeline(r.Context(), p, "", "pipeline-run")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	executionChanged := req.Changed
	if len(req.Changed) == 0 {
		executionChanged = make([]string, 0, len(proj.Repos))
		for _, repo := range proj.Repos {
			executionChanged = append(executionChanged, repo.Name)
		}
	}

	actor := actorFrom(r)
	runID, err := s.startProjectRun(r.Context(), p, &proj, primary, executionChanged, actor, req.IncludeManual)
	if err != nil {
		var secErr secretResolutionError
		if errors.As(err, &secErr) {
			writeError(w, http.StatusBadRequest, secErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Return immediately so the frontend doesn't timeout. Report the actual
	// status: Start records it as "queued" synchronously, before returning,
	// whenever every concurrency slot is busy - a hardcoded "running" here
	// would misreport a queued run - see run-concurrency-and-retention
	// design.md D1/D2.
	status := "running"
	if run, err := s.runStore.GetRun(runID); err == nil && run != nil {
		status = run.Status
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"project": proj.Name,
		"run_id":  runID,
		"status":  status,
		"changed": req.Changed,
	})

}

// handleRunTask godoc
// @Summary Start a run containing a single task
// @Description Accepts the run and returns at once; the task executes in the background. Read the outcome from GET /api/projects/{name}/runs/{runID}.
// @Tags runs
// @Produce json
// @Param name path string true "Project name"
// @Param task path string true "Task name"
// @Success 200 {object} map[string]interface{} "run_id, project, task, and accepted status (running or queued)"
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/projects/{name}/tasks/{task}/run [post]
func (s *Server) handleRunTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	taskName := chi.URLParam(r, "task")
	p, err := s.projectStore.Get(name)
	if err != nil || p == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	runService := s.runService
	if runService == nil {
		writeError(w, http.StatusInternalServerError, "run service not configured")
		return
	}
	if err := runService.CheckLocalAllowed(p.Mode); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	proj, primary, err := s.loadProjectPipeline(r.Context(), p, "", "single-task-run")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Find the task.
	found := false
	for i := range proj.Tasks {
		if proj.Tasks[i].Name == taskName {
			found = true
			break
		}
	}
	if !found {
		primary.Cleanup()
		writeError(w, http.StatusNotFound, fmt.Sprintf("task %q not found", taskName))
		return
	}

	actor := actorFrom(r)
	varStore, resolvedSecrets, err := service.BuildVarStore(r.Context(), &proj, s.secretResolver())
	if err != nil {
		primary.Cleanup()
		s.failRunOnSecretError(name, &proj, nil, taskName, false, actor, err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var runID int
	snap := &proj
	runID, err = s.runStore.CreateRun(name, nil, taskName, false, snap, actor)
	if err != nil {
		primary.Cleanup()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Return as soon as the run is accepted: StartTask records it queued or
	// running synchronously and executes the task in the background, so a
	// re-run started while every slot is busy never holds this request
	// open - see ui-single-task-rerun design.md D1/D3. The outcome is read
	// from GET /api/projects/{name}/runs/{runID} afterwards.
	queued, err := runService.StartTask(context.Background(), runID, name, &proj, taskName, domain.RunnerOptions{
		WorkDir:              primary.Root,
		Mode:                 p.Mode,
		Variables:            varStore,
		CredentialResolver:   s.credentialResolver(),
		SourceSessions:       s.sourceSessions,
		InitialSources:       []domain.AcquiredSource{primary.Source},
		WorkspaceCleanup:     primary.Cleanup,
		Stdout:               io.Discard,
		Stderr:               io.Discard,
		Actor:                actor,
		ResolvedSecretValues: resolvedSecrets,
	})
	if err != nil {
		primary.Cleanup()
		if finishErr := s.runStore.FinishRun(runID, "failed", "", err.Error(), nil); finishErr != nil {
			slog.Warn("could not finish run record", "run_id", runID, "err", redact.Err(finishErr, s.knownSecrets()))
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status := "running"
	if queued {
		status = "queued"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"run_id":  runID,
		"project": proj.Name,
		"task":    taskName,
		"status":  status,
	})
}

// ─── Run Handlers ───────────────────────────────────────────────

// handleListRuns godoc
// @Summary List pipeline runs for a project
// @Tags runs
// @Produce json
// @Param name path string true "Project name"
// @Param limit query int false "Items per page (10, 25, 50, 100; default 25)"
// @Param offset query int false "Offset for pagination (default 0)"
// @Success 200 {object} map[string]interface{} "paginated run list"
// @Router /api/projects/{name}/runs [get]
func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	limit, offset, errStr, status := pagination.Parse(r.URL.Query())
	if errStr != "" {
		http.Error(w, errStr, status)
		return
	}
	total, err := s.runStore.CountRuns(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	runs, err := s.runStore.ListRuns(name, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if runs == nil {
		runs = []domain.Run{}
	}
	runPtrs := make([]*domain.Run, len(runs))
	for i := range runs {
		runPtrs[i] = &runs[i]
	}
	s.annotateWaitingReasons(runPtrs...)
	pathPrefix := fmt.Sprintf("/api/projects/%s/runs", name)
	next, prev := pagination.Links(offset, limit, total, pathPrefix)
	resp := map[string]interface{}{
		"items":      runs,
		"pagination": pagination.Meta{Total: total, Limit: limit, Offset: offset, Next: next, Prev: prev},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleGetRun godoc
// @Summary Get a pipeline run by ID
// @Tags runs
// @Produce json
// @Param name path string true "Project name"
// @Param runID path int true "Run ID"
// @Success 200 {object} store.Run
// @Router /api/projects/{name}/runs/{runID} [get]
func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	runID := 0
	fmt.Sscanf(chi.URLParam(r, "runID"), "%d", &runID)
	if runID == 0 {
		writeError(w, http.StatusBadRequest, "invalid run ID")
		return
	}
	// The role check ran against the project named in the URL, so the run
	// must belong to that project or a grant on project A could read B's runs.
	run, err := s.runStore.GetRun(runID)
	if err != nil || run == nil || run.ProjectName != chi.URLParam(r, "name") {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	s.annotateWaitingReasons(run)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

// annotateWaitingReasons sets waiting_reason on the latest result of each
// unmatched pending task in runs (remote-unmatched-job-alert design D2).
func (s *Server) annotateWaitingReasons(runs ...*domain.Run) {
	if s.remoteJobStore == nil || len(runs) == 0 {
		return
	}
	hasPending := false
	for _, r := range runs {
		if r == nil {
			continue
		}
		for _, tr := range r.TaskResults {
			if tr.Status == domain.RemoteJobStatusPending {
				hasPending = true
				break
			}
		}
		if hasPending {
			break
		}
	}
	if !hasPending {
		return
	}

	var runIDFilter *int
	if len(runs) == 1 && runs[0] != nil {
		id := runs[0].ID
		runIDFilter = &id
	}
	unmatched, err := s.remoteJobStore.UnmatchedPendingJobs(runIDFilter)
	if err != nil || len(unmatched) == 0 {
		return
	}

	hasRunners := false
	if s.runnerStore != nil {
		if runners, err := s.runnerStore.ListRunners(); err == nil && len(runners) > 0 {
			hasRunners = true
		}
	}

	type jobKey struct {
		runID    int
		taskName string
	}
	unmatchedMap := make(map[jobKey]domain.UnmatchedPendingJob, len(unmatched))
	for _, u := range unmatched {
		unmatchedMap[jobKey{runID: u.RunID, taskName: u.TaskName}] = u
	}

	for _, r := range runs {
		if r == nil {
			continue
		}
		latestIdx := make(map[string]int)
		for i, tr := range r.TaskResults {
			latestIdx[tr.Name] = i
		}
		for taskName, idx := range latestIdx {
			if r.TaskResults[idx].Status != domain.RemoteJobStatusPending {
				continue
			}
			job, ok := unmatchedMap[jobKey{runID: r.ID, taskName: taskName}]
			if !ok {
				continue
			}
			r.TaskResults[idx].WaitingReason = waitingReason(job.RequiredTags, hasRunners)
		}
	}
}

func waitingReason(tags []string, hasRunners bool) string {
	if !hasRunners || len(tags) == 0 {
		return "no runner is registered"
	}
	sorted := append([]string(nil), tags...)
	slices.Sort(sorted)
	return fmt.Sprintf("no registered runner has tags: %s", strings.Join(sorted, ", "))
}

// handleApproveManualTask runs a manual task that is waiting for approval on a run.
func (s *Server) handleApproveManualTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	taskName := chi.URLParam(r, "task")
	runID := 0
	fmt.Sscanf(chi.URLParam(r, "runID"), "%d", &runID)
	if runID == 0 {
		writeError(w, http.StatusBadRequest, "invalid run ID")
		return
	}

	run, err := s.runStore.GetRun(runID)
	if err != nil || run == nil || run.ProjectName != name {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	// Best-effort only: approval uses the run's own stored pipeline snapshot
	// and does not otherwise require the live project, so a lookup failure
	// here just leaves Mode unset - the executor falls back to the
	// instance-wide default the same way CheckLocalAllowed below does. If
	// that default resolves to `local` without the opt-in, the empty Mode
	// now gets the approval rejected rather than silently proceeding.
	var mode string
	if p, err := s.projectStore.Get(name); err == nil && p != nil {
		mode = p.Mode
	}
	runService := s.runService
	if runService == nil {
		writeError(w, http.StatusInternalServerError, "run service not configured")
		return
	}
	if err := runService.CheckLocalAllowed(mode); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	latest := make(map[string]domain.TaskResult)
	for _, tr := range run.TaskResults {
		latest[tr.Name] = tr
	}
	cur, ok := latest[taskName]
	if !ok {
		writeError(w, http.StatusNotFound, "task not in run")
		return
	}
	isWaiting := cur.Status == "manual" ||
		(cur.Status == "skipped" && cur.Error != "dependency failed" && cur.Error != "dependency canceled")
	if !isWaiting {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("task %q is not waiting for manual approval (status=%s)", taskName, cur.Status))
		return
	}

	if run.PipelineDef == nil {
		writeError(w, http.StatusBadRequest, "run has no pipeline snapshot")
		return
	}
	proj := *run.PipelineDef
	workDir, repoPaths, cleanup, err := s.openRecordedRunSources(r.Context(), run)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	varStore, resolvedSecrets, err := service.BuildVarStore(r.Context(), &proj, s.secretResolver())
	if err != nil {
		cleanup()
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if _, err := runService.ApproveManualTask(context.Background(), runID, name, &proj, taskName, domain.RunnerOptions{
		WorkDir: workDir, RepoPaths: repoPaths, SourcesPrepared: true, Mode: mode,
		Variables: varStore, CredentialResolver: s.credentialResolver(),
		SourceSessions: s.sourceSessions, WorkspaceCleanup: cleanup,
		Actor: actorFrom(r), ResolvedSecretValues: resolvedSecrets,
	}); err != nil {
		cleanup()
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"run_id":  runID,
		"task":    taskName,
		"status":  "running",
		"project": name,
	})
}

// handleCancelRun cancels a running pipeline run. If the run is actively
// executing, Cancel terminates its task before the store records it as
// canceled; a run stuck beyond its grace period stays "running" and this
// reports 504 rather than falsely claiming the run stopped.
func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	runID := 0
	fmt.Sscanf(chi.URLParam(r, "runID"), "%d", &runID)
	if runID == 0 {
		writeError(w, http.StatusBadRequest, "invalid run ID")
		return
	}
	if s.runService == nil {
		writeError(w, http.StatusInternalServerError, "run service not configured")
		return
	}
	// The role check ran against the project named in the URL, so the run
	// must belong to that project or a grant on project A could cancel B's runs.
	run, err := s.runStore.GetRun(runID)
	if err != nil || run == nil || run.ProjectName != chi.URLParam(r, "name") {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if err := s.runService.Cancel(r.Context(), runID, actorFrom(r)); err != nil {
		if errors.Is(err, service.ErrRunCancelTimedOut) {
			writeError(w, http.StatusGatewayTimeout, fmt.Sprintf("run %d did not stop in time", runID))
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.sourceSessions != nil {
		_ = s.sourceSessions.Release(context.Background(), runID)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "canceled"})
}

// handleListAllRuns godoc
// @Summary List all pipeline runs across projects
// @Tags runs
// @Produce json
// @Success 200 {array} store.Run
// @Router /api/runs [get]
func (s *Server) handleListAllRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.runStore.ListAllRuns(100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if runs == nil {
		runs = []domain.Run{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

// failRunOnSecretError records a run that never executed a single task
// because service.BuildVarStore could not resolve one of its secret
// variables (runtime-secrets design.md D3/D6): the run is created, every one
// of its tasks is recorded skipped, and the run finishes failed with
// secretErr's message - so the failure is visible in run history and not
// only in this request's 400 response, and the pipeline runner is never
// invoked.
func (s *Server) failRunOnSecretError(projectName string, proj *domain.Project, changed []string, rerunTask string, includeManual bool, actor string, secretErr error) {
	if s.runStore == nil {
		return
	}
	startedAt := time.Now()
	runID, err := s.runStore.CreateRun(projectName, changed, rerunTask, includeManual, proj, actor)
	if err != nil {
		slog.Warn("could not record run for secret resolution failure", "project", projectName, "err", redact.Err(err, s.knownSecrets()))
		return
	}
	taskResults := make([]domain.TaskResult, 0, len(proj.Tasks))
	for _, t := range proj.Tasks {
		taskResults = append(taskResults, domain.TaskResult{Name: t.Name, Status: "skipped", Error: secretErr.Error()})
	}
	if err := s.runStore.FinishRun(runID, "failed", "", secretErr.Error(), taskResults); err != nil {
		slog.Warn("could not finish run record for secret resolution failure", "run_id", runID, "project", projectName, "err", redact.Err(err, s.knownSecrets()))
		return
	}
	if s.metrics != nil {
		s.metrics.RunFinished("failed", projectName, time.Since(startedAt))
	}
}

type secretResolutionError string

func (e secretResolutionError) Error() string { return string(e) }

// startProjectRun initiates a pipeline run from an already-loaded project
// pipeline and source session (design D1). It resolves variables and secrets
// through BuildVarStore, records a failed run on secret resolution error, and
// starts the run through RunService.Start. The session's cleanup is handed to
// the run on success or called here on failure.
func (s *Server) startProjectRun(ctx context.Context, cfg *domain.ProjectConfig, proj *domain.Project, session projectSourceSession, changed []string, actor string, includeManual ...bool) (int, error) {
	var incManual bool
	if len(includeManual) > 0 {
		incManual = includeManual[0]
	}
	varStore, resolvedSecrets, err := service.BuildVarStore(ctx, proj, s.secretResolver())
	if err != nil {
		session.Cleanup()
		s.failRunOnSecretError(cfg.Name, proj, changed, "", incManual, actor, err)
		return 0, secretResolutionError(err.Error())
	}
	runID, err := s.runService.Start(context.Background(), cfg.Name, proj, changed, domain.RunnerOptions{
		WorkDir:              session.Root,
		IncludeManual:        incManual,
		Variables:            varStore,
		Mode:                 cfg.Mode,
		CredentialResolver:   s.credentialResolver(),
		SourceSessions:       s.sourceSessions,
		InitialSources:       []domain.AcquiredSource{session.Source},
		WorkspaceCleanup:     session.Cleanup,
		Actor:                actor,
		ResolvedSecretValues: resolvedSecrets,
	})
	if err != nil {
		session.Cleanup()
		return 0, err
	}
	return runID, nil
}
