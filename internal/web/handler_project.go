package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"kestrix/internal/dag"
	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/parser"
	"kestrix/internal/service"
	"kestrix/internal/web/pagination"

	"github.com/go-chi/chi/v5"
)

var absPath = filepath.Abs

// ─── Project Handlers (use FileStore) ─────────────────────────

// handleListProjects godoc
// @Summary List configured projects
// @Tags projects
// @Produce json
// @Param limit query int false "Items per page (10, 25, 50, 100; default 25)"
// @Param offset query int false "Offset for pagination (default 0)"
// @Success 200 {object} map[string]interface{} "paginated project list"
// @Router /api/projects [get]
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	limit, offset, errStr, status := pagination.Parse(r.URL.Query())
	if errStr != "" {
		http.Error(w, errStr, status)
		return
	}
	// Load all projects (could be optimized)
	projMap, err := s.projectService.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Convert map to slice for sorting
	list := make([]*domain.ProjectConfig, 0, len(projMap))
	for _, p := range projMap {
		list = append(list, p)
	}
	// Sort by name (existing behavior)
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	total := len(list)
	// Apply pagination slice
	start := offset
	if start > total {
		start = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	paged := list[start:end]
	next, prev := pagination.Links(offset, limit, total, "/api/projects")
	resp := map[string]interface{}{
		"items":      paged,
		"pagination": pagination.Meta{Total: total, Limit: limit, Offset: offset, Next: next, Prev: prev},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleCreateProject godoc
// @Summary Create a project (point at a repo and its pipeline definition directory)
// @Tags projects
// @Accept json
// @Produce json
// @Param request body domain.ProjectConfig true "Project config"
// @Success 201 {object} domain.ProjectConfig
// @Router /api/projects [post]
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req domain.ProjectConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if err := s.projectService.Create(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(req)
}

// handleGetProject godoc
// @Summary Get a project config
// @Tags projects
// @Produce json
// @Param name path string true "Project name"
// @Success 200 {object} domain.ProjectConfig
// @Router /api/projects/{name} [get]
func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	p, err := s.projectService.Get(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

// handleUpdateProject godoc
// @Summary Update a project config
// @Tags projects
// @Accept json
// @Produce json
// @Param name path string true "Project name"
// @Param request body domain.ProjectConfig true "Updated config"
// @Success 200 {object} domain.ProjectConfig
// @Router /api/projects/{name} [put]
func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var update domain.ProjectConfig
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.projectService.Update(name, &update); err != nil {
		if errors.Is(err, service.ErrValidation) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(update)
}

// handleDeleteProject godoc
// @Summary Delete a project config
// @Tags projects
// @Produce json
// @Param name path string true "Project name"
// @Success 200 {object} map[string]interface{}
// @Router /api/projects/{name} [delete]
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := s.projectService.Delete(name); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "deleted"})
}

// projectPipelineDir resolves the directory containing a project's pipeline
// definitions and prevents a relative configuration from escaping RepoPath.
func (s *Server) projectPipelineDir(p *domain.ProjectConfig) (string, error) {
	pipelineDir := p.PipelineDir
	if pipelineDir == "" {
		pipelineDir = filepath.Dir(p.PipelineFile)
	}
	repoPath, err := absPath(p.RepoPath)
	if err != nil {
		return "", fmt.Errorf("resolve repo path: %w", err)
	}
	dir := filepath.Clean(filepath.Join(repoPath, pipelineDir))
	rel, err := filepath.Rel(repoPath, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("pipeline directory %q escapes repository", pipelineDir)
	}
	return dir, nil
}

// projectPipelinePath resolves the editor's file inside the pipeline directory.
func (s *Server) projectPipelinePath(p *domain.ProjectConfig) (string, error) {
	if p.PipelineFile == "" {
		p.PipelineFile = "pipeline.tf"
	}
	fileName := p.PipelineFile
	if p.PipelineDir == "" {
		fileName = filepath.Base(fileName)
	}
	dir, err := s.projectPipelineDir(p)
	if err != nil {
		return "", err
	}
	path := filepath.Clean(filepath.Join(dir, fileName))
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("pipeline file %q escapes pipeline directory", fileName)
	}
	return path, nil
}

// parseProjectPipeline merges all .tf definitions from the configured pipeline
// directory. Other .tf files in the repository are intentionally ignored.
func (s *Server) parseProjectPipeline(p *domain.ProjectConfig) (model.Project, error) {
	dir, err := s.projectPipelineDir(p)
	if err != nil {
		return model.Project{}, err
	}
	proj, err := parser.ParseDir(dir)
	if err != nil {
		return model.Project{}, fmt.Errorf("parse pipeline directory %s: %w", dir, err)
	}
	return proj, nil
}

// handleGetProjectPipeline godoc
// @Summary Get raw pipeline HCL for a project
// @Tags projects
// @Produce text/plain
// @Param name path string true "Project name"
// @Success 200 {string} string
// @Router /api/projects/{name}/pipeline [get]
func (s *Server) handleGetProjectPipeline(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	p, err := s.projectStore.Get(name)
	if err != nil || p == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	path, err := s.projectPipelinePath(p)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

// handleGetProjectDefinition returns the parsed configured pipeline file.
func (s *Server) handleGetProjectDefinition(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(proj)
}

// handleSaveProjectPipeline godoc
// @Summary Save the editor pipeline file inside a project's pipeline directory
// @Tags projects
// @Accept plain
// @Produce json
// @Param name path string true "Project name"
// @Success 200 {object} map[string]interface{}
// @Router /api/projects/{name}/pipeline [post]
func (s *Server) handleSaveProjectPipeline(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	p, err := s.projectStore.Get(name)
	if err != nil || p == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body")
		return
	}
	pipelineDir, err := s.projectPipelineDir(p)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	path, err := s.projectPipelinePath(p)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "saved",
		"path":   path,
	})
}

// handleResolveProject godoc
// @Summary Resolve a project's triggers against changed repos
// @Tags projects
// @Accept json
// @Produce json
// @Param name path string true "Project name"
// @Param request body object true "Changed repos"
// @Success 200 {object} map[string]interface{}
// @Router /api/projects/{name}/resolve [post]
func (s *Server) handleResolveProject(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	p, err := s.projectStore.Get(name)
	if err != nil || p == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	var req struct {
		Changed []string `json:"changed"`
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
	var triggered []map[string]interface{}
	for _, n := range nodes {
		if n.Triggered {
			triggered = append(triggered, map[string]interface{}{
				"name":   n.Task.Name,
				"reason": n.TriggeredReason,
				"manual": n.Task.Manual,
			})
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
