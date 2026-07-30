package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gitlab.com/lunaris-bot/metaci/internal/dag"
	"gitlab.com/lunaris-bot/metaci/internal/parser"
)

var readPipelineFile = os.ReadFile

// handleGetPipeline godoc
// @Summary Parse and return pipeline definition
// @Tags pipeline
// @Produce json
// @Param dir query string false "Pipeline directory"
// @Success 200 {object} model.Project
// @Router /api/pipeline [get]
func (s *Server) handleGetPipeline(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = s.cfg.PipelineDir
	}

	proj, err := parser.ParseDir(dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(proj)
}

// handleGetPipelineRaw godoc
// @Summary Return raw pipeline HCL source
// @Tags pipeline
// @Produce text/plain
// @Param dir query string false "Pipeline directory"
// @Success 200 {string} string
// @Router /api/pipeline/raw [get]
func (s *Server) handleGetPipelineRaw(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = s.cfg.PipelineDir
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var tfFile string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".tf") {
			tfFile = filepath.Join(dir, e.Name())
			break
		}
	}
	if tfFile == "" {
		writeError(w, http.StatusNotFound, "no .tf file found")
		return
	}

	data, err := readPipelineFile(tfFile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

// handleResolve godoc
// @Summary Resolve trigger expressions against changed repos
// @Tags pipeline
// @Accept json
// @Produce json
// @Param request body object true "Changed repos"
// @Success 200 {object} map[string]interface{}
// @Router /api/resolve [post]
func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Changed []string `json:"changed"`
		Dir     string   `json:"dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Dir == "" {
		req.Dir = s.cfg.PipelineDir
	}

	proj, err := parser.ParseDir(req.Dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	changedSet := make(map[string]bool)
	for _, c := range req.Changed {
		changedSet[c] = true
	}

	e := dag.New(proj)
	nodes, err := e.Resolve(changedSet)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var triggered []string
	for _, n := range nodes {
		if n.Triggered {
			triggered = append(triggered, n.Task.Name)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"project":   proj.Name,
		"changed":   req.Changed,
		"triggered": triggered,
		"all_tasks": len(proj.Tasks),
	})
}

// handleGenerate godoc
// @Summary Generate gitlab-ci.yml from pipeline definition
// @Tags pipeline
// @Accept json
// @Produce json
// @Param request body object true "Generation options"
// @Success 200 {object} map[string]interface{}
// @Router /api/generate [post]
func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir     string   `json:"dir"`
		Changed []string `json:"changed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Dir == "" {
		req.Dir = s.cfg.PipelineDir
	}

	proj, err := parser.ParseDir(req.Dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	e := dag.New(proj)
	changedSet := make(map[string]bool)
	for _, c := range req.Changed {
		changedSet[c] = true
	}

	nodes, err := e.Resolve(changedSet)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var triggered []dag.Node
	for _, n := range nodes {
		if n.Triggered {
			triggered = append(triggered, n)
		}
	}

	yaml := GenerateGitLabCI(proj, triggered)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"yaml":       yaml,
		"task_count": len(triggered),
	})
}

// handleValidate godoc
// @Summary Validate pipeline definition
// @Tags pipeline
// @Produce json
// @Param dir query string false "Pipeline directory"
// @Success 200 {object} map[string]interface{}
// @Router /api/validate [get]
func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = s.cfg.PipelineDir
	}

	proj, err := parser.ParseDir(dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	e := dag.New(proj)
	if err := e.Validate(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"valid":  false,
			"errors": []string{err.Error()},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"valid":   true,
		"project": proj.Name,
		"repos":   len(proj.Repos),
		"tasks":   len(proj.Tasks),
	})
}
