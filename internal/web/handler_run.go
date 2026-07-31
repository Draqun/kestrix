package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"kestrix/internal/domain"

	"kestrix/internal/web/pagination"

	"github.com/go-chi/chi/v5"
)

// handleRunProject executes a registered project's selected tasks in its local
// checkout. This endpoint intentionally accepts only a project name, rather
// than a path or command, so execution stays limited to registered projects
// and the scripts committed in their pipeline definition.
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	proj, err := s.parseProjectPipeline(p)
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

	varStore := s.buildVarStore(&proj)
	runService := s.runService
	if runService == nil {
		writeError(w, http.StatusInternalServerError, "run service not configured")
		return
	}
	runID, err := runService.Start(context.Background(), name, &proj, executionChanged, domain.RunnerOptions{
		WorkDir: p.RepoPath, IncludeManual: req.IncludeManual, Variables: varStore,
	})
	if err != nil {
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
	proj, err := s.parseProjectPipeline(p)
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
		writeError(w, http.StatusNotFound, fmt.Sprintf("task %q not found", taskName))
		return
	}

	varStore := s.buildVarStore(&proj)
	runService := s.runService
	if runService == nil {
		writeError(w, http.StatusInternalServerError, "run service not configured")
		return
	}

	var runID int
	snap := &proj
	runID, err = s.runStore.CreateRun(name, []string{taskName}, false, snap)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	tr, err := runService.RunTask(r.Context(), runID, name, &proj, taskName, domain.RunnerOptions{
		WorkDir:   p.RepoPath,
		Variables: varStore,
		Stdout:    io.Discard,
		Stderr:    io.Discard,
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
	if err := s.runStore.FinishRun(runID, status, tr.Output, errMsg, []domain.TaskResult{tr}); err != nil {
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
	run, err := s.runStore.GetRun(runID)
	if err != nil || run == nil {
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

	p, err := s.projectStore.Get(name)
	if err != nil || p == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	proj, err := s.parseProjectPipeline(p)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	runService := s.runService
	if runService == nil {
		writeError(w, http.StatusInternalServerError, "run service not configured")
		return
	}

	if _, err := runService.ApproveManualTask(context.Background(), runID, name, &proj, taskName, domain.RunnerOptions{WorkDir: p.RepoPath, Variables: s.buildVarStore(&proj)}); err != nil {
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

// handleCancelRun cancels a running pipeline run.
func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	runID := 0
	fmt.Sscanf(chi.URLParam(r, "runID"), "%d", &runID)
	if runID == 0 {
		writeError(w, http.StatusBadRequest, "invalid run ID")
		return
	}
	if err := s.runStore.CancelRun(runID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
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
