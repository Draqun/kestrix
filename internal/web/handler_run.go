package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"

	"kestrix/internal/domain"
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
	varStore, resolvedSecrets, err := service.BuildVarStore(r.Context(), &proj, s.secretResolver())
	if err != nil {
		primary.Cleanup()
		s.failRunOnSecretError(name, &proj, executionChanged, req.IncludeManual, actor, err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	runService := s.runService
	if runService == nil {
		primary.Cleanup()
		writeError(w, http.StatusInternalServerError, "run service not configured")
		return
	}
	runID, err := runService.Start(context.Background(), name, &proj, executionChanged, domain.RunnerOptions{
		WorkDir: primary.Root, IncludeManual: req.IncludeManual, Variables: varStore,
		CredentialResolver:   s.credentialResolver(),
		SourceSessions:       s.sourceSessions,
		InitialSources:       []domain.AcquiredSource{primary.Source},
		WorkspaceCleanup:     primary.Cleanup,
		Actor:                actor,
		ResolvedSecretValues: resolvedSecrets,
	})
	if err != nil {
		primary.Cleanup()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Return immediately so the frontend doesn't timeout.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"project": proj.Name,
		"run_id":  runID,
		"status":  "running",
		"changed": req.Changed,
	})

}

// handleRunTask executes a single task by name.
func (s *Server) handleRunTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	taskName := chi.URLParam(r, "task")
	p, err := s.projectStore.Get(name)
	if err != nil || p == nil {
		writeError(w, http.StatusNotFound, "project not found")
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

	runService := s.runService
	if runService == nil {
		primary.Cleanup()
		writeError(w, http.StatusInternalServerError, "run service not configured")
		return
	}

	actor := actorFrom(r)
	varStore, resolvedSecrets, err := service.BuildVarStore(r.Context(), &proj, s.secretResolver())
	if err != nil {
		primary.Cleanup()
		s.failRunOnSecretError(name, &proj, []string{taskName}, false, actor, err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var runID int
	snap := &proj
	runID, err = s.runStore.CreateRun(name, []string{taskName}, false, snap, actor)
	if err != nil {
		primary.Cleanup()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	tr, err := runService.RunTask(r.Context(), runID, name, &proj, taskName, domain.RunnerOptions{
		WorkDir:              primary.Root,
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
	status := "passed"
	errMsg := ""
	if err != nil {
		status = "failed"
		errMsg = err.Error()
	}
	if tr.Status == "failed" {
		status = "failed"
		if errMsg == "" {
			errMsg = tr.Error
		}
	}
	// Cancel records its outcome via CancelRun once RunTask's done channel
	// closes, which happens before this point; do not overwrite a run it
	// already marked canceled with this handler's local verdict — in the
	// stored record or in this response.
	if run, _ := s.runStore.GetRun(runID); run != nil && run.Status == "canceled" {
		log.Printf("run: task finished after cancel, keeping canceled status run=%d", runID)
		status, errMsg = "canceled", "canceled"
	} else if err := s.runStore.FinishRun(runID, status, tr.Output, errMsg, []domain.TaskResult{tr}); err != nil {
		log.Printf("warning: could not finish run record: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"project": proj.Name,
		"task":    taskName,
		"status":  status,
		"output":  tr.Output,
	}
	if errMsg != "" {
		response["error"] = errMsg
	}
	json.NewEncoder(w).Encode(response)
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
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

	runService := s.runService
	if runService == nil {
		writeError(w, http.StatusInternalServerError, "run service not configured")
		return
	}

	varStore, resolvedSecrets, err := service.BuildVarStore(r.Context(), &proj, s.secretResolver())
	if err != nil {
		cleanup()
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if _, err := runService.ApproveManualTask(context.Background(), runID, name, &proj, taskName, domain.RunnerOptions{
		WorkDir: workDir, RepoPaths: repoPaths, SourcesPrepared: true,
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
func (s *Server) failRunOnSecretError(projectName string, proj *domain.Project, changed []string, includeManual bool, actor string, secretErr error) {
	if s.runStore == nil {
		return
	}
	runID, err := s.runStore.CreateRun(projectName, changed, includeManual, proj, actor)
	if err != nil {
		log.Printf("warning: could not record run for secret resolution failure: %v", err)
		return
	}
	taskResults := make([]domain.TaskResult, 0, len(proj.Tasks))
	for _, t := range proj.Tasks {
		taskResults = append(taskResults, domain.TaskResult{Name: t.Name, Status: "skipped", Error: secretErr.Error()})
	}
	if err := s.runStore.FinishRun(runID, "failed", "", secretErr.Error(), taskResults); err != nil {
		log.Printf("warning: could not finish run record for secret resolution failure: %v", err)
	}
}
