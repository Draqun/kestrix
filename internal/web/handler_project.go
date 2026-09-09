package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
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

// decodeProjectConfig reads and decodes r's body into cfg, answering 413 when
// the body exceeded the route's configured bound (see maxBody) and 400 on any
// other read or parse failure. It reports whether decoding succeeded; on
// false the caller must return without running its business logic - the
// response has already been written.
func decodeProjectConfig(w http.ResponseWriter, r *http.Request, cfg *domain.ProjectConfig) bool {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

// publicProject returns a copy of cfg fit for the wire: WebhookSecret
// blanked and HasWebhookSecret set to whether a secret is stored. Every
// project encode site (create, list, detail, update) MUST go through this -
// see design.md D4.
func publicProject(cfg *domain.ProjectConfig) domain.ProjectConfig {
	pub := *cfg
	pub.HasWebhookSecret = cfg.WebhookSecret != ""
	pub.WebhookSecret = ""
	return pub
}

// ─── Project Handlers (use PostgreSQL) ─────────────────────────

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
	items := make([]domain.ProjectConfig, len(paged))
	for i, p := range paged {
		items[i] = publicProject(p)
	}
	next, prev := pagination.Links(offset, limit, total, "/api/projects")
	resp := map[string]interface{}{
		"items":      items,
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
	if !decodeProjectConfig(w, r, &req) {
		return
	}

	if err := s.projectService.CreateValidated(&req, func(candidate *domain.ProjectConfig) (model.Project, error) {
		proj, session, err := s.loadProjectPipeline(r.Context(), candidate, "", "project-create-validation")
		if err == nil {
			session.Cleanup()
		}
		return proj, err
	}); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(publicProject(&req))
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
	json.NewEncoder(w).Encode(publicProject(p))
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
	if !decodeProjectConfig(w, r, &update) {
		return
	}
	if err := s.projectService.UpdateValidated(name, &update, func(candidate *domain.ProjectConfig) (model.Project, error) {
		proj, session, err := s.loadProjectPipeline(r.Context(), candidate, "", "project-update-validation")
		if err == nil {
			session.Cleanup()
		}
		return proj, err
	}); err != nil {
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
	json.NewEncoder(w).Encode(publicProject(&update))
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
// definitions and prevents a relative configuration from escaping the
// operation's private primary checkout.
func (s *Server) projectPipelineDir(p *domain.ProjectConfig, repoRoot string) (string, error) {
	pipelineDir := p.PipelineDir
	if pipelineDir == "" {
		pipelineDir = filepath.Dir(p.PipelineFile)
	}
	repoPath, err := absPath(repoRoot)
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
func (s *Server) projectPipelinePath(p *domain.ProjectConfig, repoRoot string) (string, error) {
	if p.PipelineFile == "" {
		p.PipelineFile = "pipeline.tf"
	}
	fileName := p.PipelineFile
	if p.PipelineDir == "" {
		fileName = filepath.Base(fileName)
	}
	dir, err := s.projectPipelineDir(p, repoRoot)
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
	proj, session, err := s.loadProjectPipeline(context.Background(), p, "", "project-definition")
	if err != nil {
		return model.Project{}, err
	}
	defer session.Cleanup()
	return proj, nil
}

func (s *Server) loadProjectPipeline(ctx context.Context, p *domain.ProjectConfig, revision, purpose string) (model.Project, projectSourceSession, error) {
	session, err := s.openProjectSource(ctx, p, revision, purpose)
	if err != nil {
		return model.Project{}, projectSourceSession{}, err
	}
	dir, err := s.projectPipelineDir(p, session.Root)
	if err != nil {
		session.Cleanup()
		return model.Project{}, projectSourceSession{}, err
	}
	proj, err := parser.ParseDir(dir)
	if err != nil {
		session.Cleanup()
		return model.Project{}, projectSourceSession{}, fmt.Errorf("parse pipeline directory: %w", err)
	}
	return proj, session, nil
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
	session, err := s.openProjectSource(r.Context(), p, "", "project-pipeline-read")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer session.Cleanup()
	path, err := s.projectPipelinePath(p, session.Root)
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
	proj, session, err := s.loadProjectPipeline(r.Context(), p, "", "project-definition-read")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer session.Cleanup()
	s.refreshSources(p, proj)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(proj)
}

// refreshSources re-derives a project's sources from a pipeline just loaded
// from its primary repository and stores them when they differ from what is
// recorded; an unchanged definition costs no write. Failing to record them is
// logged, not fatal: the caller already has the definition, and the next
// load retries.
func (s *Server) refreshSources(p *domain.ProjectConfig, proj model.Project) {
	derived := service.DeriveSources(p, proj)
	if slices.Equal(p.Sources, derived) {
		return
	}
	if err := s.projectStore.ReplaceSources(p.Name, derived); err != nil {
		log.Printf("project %s: record sources: %v", p.Name, err)
		return
	}
	p.Sources = derived
}

// handleSaveProjectPipeline godoc
// @Summary Reject writes to Git-owned registered pipeline content
// @Tags projects
// @Accept plain
// @Produce json
// @Param name path string true "Project name"
// @Failure 405 {object} map[string]interface{}
// @Router /api/projects/{name}/pipeline [post]
func (s *Server) handleSaveProjectPipeline(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	p, err := s.projectStore.Get(name)
	if err != nil || p == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "registered pipeline content is read-only; update it in the remote Git repository")
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
	if !decodeJSON(w, r, &req) {
		return
	}
	proj, session, err := s.loadProjectPipeline(r.Context(), p, "", "project-resolve")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer session.Cleanup()
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
