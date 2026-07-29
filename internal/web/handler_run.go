package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"gitlab.com/lunaris-bot/metaci/internal/domain"
	"gitlab.com/lunaris-bot/metaci/internal/model"
	"gitlab.com/lunaris-bot/metaci/internal/runner"
	"gitlab.com/lunaris-bot/metaci/internal/service"

	"gitlab.com/lunaris-bot/metaci/internal/web/pagination"

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
	if s.runService == nil {
		s.runService = service.NewRunService(s.runStore, &runner.PipelineExecutor{
			Mode: s.cfg.Runner.Mode, GenerateYAML: GenerateGitLabCI,
		})
	}
	runID, err := s.runService.Start(context.Background(), name, &proj, executionChanged, domain.RunnerOptions{
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
	var targetTask *model.Task
	for i := range proj.Tasks {
		if proj.Tasks[i].Name == taskName {
			targetTask = &proj.Tasks[i]
			break
		}
	}
	if targetTask == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task %q not found", taskName))
		return
	}

	// Build changed set — mark the task's trigger repos as changed.
	changedSet := make(map[string]bool)
	for _, repo := range proj.Repos {
		changedSet[repo.Name] = false
	}
	trig := targetTask.Trigger
	for _, repo := range proj.Repos {
		if strings.Contains(trig, repo.Name) {
			changedSet[repo.Name] = true
		}
	}

	varStore := s.buildVarStore(&proj)

	// Persist run.
	var runID int
	if s.runStore != nil {
		snap := &proj
		var runErr error
		runID, runErr = s.runStore.CreateRun(name, []string{taskName}, false, snap)
		if runErr != nil {
			log.Printf("warning: could not create run record: %v", runErr)
		}
	}

	var output bytes.Buffer
	var taskResults []domain.TaskResult
	if s.cfg.Runner.Mode == "local" {
		taskResults, err = runner.RunLocal(r.Context(), proj, changedSet, runner.Options{
			WorkDir:       p.RepoPath,
			IncludeManual: true,
			Variables:     varStore,
			Stdout:        &output,
			Stderr:        &output,
		})
	} else {
		taskResults, err = runner.Run(r.Context(), proj, changedSet, runner.Options{
			WorkDir:       p.RepoPath,
			IncludeManual: true,
			Variables:     varStore,
			Stdout:        &output,
			Stderr:        &output,
		})
	}

	status := "passed"
	errMsg := ""
	if err != nil {
		status = "failed"
		errMsg = err.Error()
	}
	if s.runStore != nil && runID > 0 {
		var trs []domain.TaskResult
		for _, tr := range taskResults {
			trs = append(trs, domain.TaskResult{
				Name: tr.Name, Status: tr.Status, Output: tr.Output,
				Error: tr.Error, Duration: tr.Duration,
			})
		}
		s.runStore.FinishRun(runID, status, output.String(), errMsg, trs)
	}

	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"project": proj.Name,
		"task":    taskName,
		"status":  status,
		"output":  output.String(),
	}
	if err != nil {
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
	if s.runStore == nil {
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}
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
	if s.runStore == nil {
		writeError(w, http.StatusBadRequest, "runs not supported")
		return
	}
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
	if s.runStore == nil {
		writeError(w, http.StatusBadRequest, "runs not supported")
		return
	}
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

	// Latest status per task.
	latest := make(map[string]domain.TaskResult)
	for _, tr := range run.TaskResults {
		latest[tr.Name] = tr
	}
	cur, ok := latest[taskName]
	if !ok {
		writeError(w, http.StatusNotFound, "task not in run")
		return
	}
	// Allow approve for "manual", or legacy "skipped" manual jobs (no dependency error).
	isWaiting := cur.Status == "manual" ||
		(cur.Status == "skipped" && cur.Error != "dependency failed" && cur.Error != "dependency canceled")
	if !isWaiting {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("task %q is not waiting for manual approval (status=%s)", taskName, cur.Status))
		return
	}

	// Confirm task is manual in snapshot or live pipeline.
	manualOK := false
	var dependsOn []string
	if run.PipelineDef != nil {
		for _, t := range run.PipelineDef.Tasks {
			if t.Name == taskName {
				manualOK = t.Manual
				dependsOn = t.DependsOn
				break
			}
		}
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
	var target *model.Task
	for i := range proj.Tasks {
		if proj.Tasks[i].Name == taskName {
			target = &proj.Tasks[i]
			if target.Manual {
				manualOK = true
			}
			if len(dependsOn) == 0 {
				dependsOn = target.DependsOn
			}
			break
		}
	}
	if target == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task %q not found in pipeline", taskName))
		return
	}
	if !manualOK {
		writeError(w, http.StatusBadRequest, "task is not manual")
		return
	}
	for _, dep := range dependsOn {
		d, exists := latest[dep]
		if !exists || d.Status != "passed" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("dependency %q has not passed", dep))
			return
		}
	}

	// Build variables (same as full run).
	varStore := s.buildVarStore(&proj)

	_ = s.runStore.ApplyTaskResult(runID, domain.TaskResult{Name: taskName, Status: "running"}, "")

	// Return immediately; execute in background (deploy can take minutes).
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"run_id":  runID,
		"task":    taskName,
		"status":  "running",
		"project": name,
	})

	workDir := p.RepoPath
	go func() {
		var output bytes.Buffer
		tr, runErr := runner.RunOne(context.Background(), proj, taskName, runner.Options{
			WorkDir:   workDir,
			Variables: varStore,
			Stdout:    &output,
			Stderr:    &output,
		})
		st := tr
		if runErr != nil && st.Status == "" {
			st.Status = "failed"
			st.Error = runErr.Error()
		}
		if err := s.runStore.ApplyTaskResult(runID, st, output.String()); err != nil {
			log.Printf("approve task %s on run #%d: %v", taskName, runID, err)
		}
	}()
}

// handleCancelRun cancels a running pipeline run.
func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	if s.runStore == nil {
		writeError(w, http.StatusBadRequest, "runs not supported")
		return
	}
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
	if s.runStore == nil {
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}
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
