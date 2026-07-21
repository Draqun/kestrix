package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gitlab.com/lunaris-bot/metaci/internal/dag"
	"gitlab.com/lunaris-bot/metaci/internal/dispatch"
	"gitlab.com/lunaris-bot/metaci/internal/model"
	"gitlab.com/lunaris-bot/metaci/internal/parser"
	"gitlab.com/lunaris-bot/metaci/internal/runner"
	"gitlab.com/lunaris-bot/metaci/internal/store"
	"gitlab.com/lunaris-bot/metaci/internal/variables"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"
	"gitlab.com/lunaris-bot/metaci/docs"
	"gitlab.com/lunaris-bot/metaci/internal/version"
)

// SignatureConfig defines webhook authentication.
// Defined in webhook_verify.go.

// DispatchConfig controls GitLab pipeline dispatch.
type DispatchConfig struct {
	Enabled     bool
	ProjectID   string
	GitLabURL   string
	GitLabToken string
}

// Config holds server configuration.
type Config struct {
	ListenAddr  string
	PipelineDir string
	StaticDir   string
	StorePath   string
	DatabaseURL string
	Branch      string
	Docker      DockerConfig
	Signature   SignatureConfig
	Dispatch    DispatchConfig
	Runner      RunnerConfig
}

// RunnerConfig holds runner execution settings.
type RunnerConfig struct {
	Mode     string // "local" or "gitlab-runner"
	Executor string // "shell", "docker", etc.
}

// DockerConfig holds Docker registry and deployment settings.
type DockerConfig struct {
	Registry string
	Image    string
	Username string
	Password string
}

// Server is the HTTP server.
type Server struct {
	cfg        Config
	router     chi.Router
	webhookLog []map[string]interface{}
	store      store.Store
}

// NewServer creates a new HTTP server.
func NewServer(cfg Config) *Server {
	s := &Server{cfg: cfg}

	// Initialize project store — PostgreSQL if DATABASE_URL is set, else file.
	if cfg.DatabaseURL != "" {
		pg, err := store.NewPG(cfg.DatabaseURL)
		if err != nil {
			log.Printf("warning: could not connect to database: %v, falling back to file store", err)
		} else {
			log.Printf("connected to PostgreSQL")
			s.store = pg
		}
	}
	if s.store == nil {
		storePath := cfg.StorePath
		if storePath == "" {
			storePath = store.DefaultPath()
		}
		fs, err := store.New(storePath)
		if err != nil {
			log.Printf("warning: could not init project store at %s: %v", storePath, err)
			fallback := filepath.Join(os.TempDir(), fmt.Sprintf("metaci-%d-projects.json", os.Getpid()))
			fs, _ = store.New(fallback)
		}
		s.store = fs
	}

	// Auto-create project from --dir on startup.
	if cfg.PipelineDir != "" {
		if proj, err := parser.ParseDir(cfg.PipelineDir); err == nil && proj.Name != "" {
			_ = s.store.Upsert(&store.ProjectConfig{
				Name:            proj.Name,
				RepoPath:        cfg.PipelineDir,
				PipelineDir:     cfg.PipelineDir,
				PipelineFile:    "pipeline.tf",
				DefaultBranch:   "main",
				GitLabProjectID: 0,
			})
		}
	}

	s.router = s.buildRouter()
	return s
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()

	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Heartbeat("/ping"))

	// API routes
	r.Route("/api", func(r chi.Router) {
		r.Get("/health", s.handleHealth)
		r.Get("/pipeline", s.handleGetPipeline)
		r.Get("/pipeline/raw", s.handleGetPipelineRaw)
		r.Get("/projects", s.handleListProjects)
		r.Post("/projects", s.handleCreateProject)
		r.Get("/projects/{name}", s.handleGetProject)
		r.Put("/projects/{name}", s.handleUpdateProject)
		r.Delete("/projects/{name}", s.handleDeleteProject)
		r.Get("/projects/{name}/definition", s.handleGetProjectDefinition)
		r.Get("/projects/{name}/pipeline", s.handleGetProjectPipeline)
		r.Post("/projects/{name}/pipeline", s.handleSaveProjectPipeline)
		r.Post("/projects/{name}/resolve", s.handleResolveProject)
		r.Post("/projects/{name}/run", s.handleRunProject)
		r.Post("/projects/{name}/tasks/{task}/run", s.handleRunTask)
		r.Get("/projects/{name}/runs", s.handleListRuns)
		r.Get("/projects/{name}/runs/{runID}", s.handleGetRun)
		r.Post("/projects/{name}/runs/{runID}/cancel", s.handleCancelRun)
		r.Post("/projects/{name}/runs/{runID}/tasks/{task}/run", s.handleApproveManualTask)
		r.Get("/runs", s.handleListAllRuns)
		r.Post("/resolve", s.handleResolve)
		r.Post("/generate", s.handleGenerate)
		r.Get("/validate", s.handleValidate)
		r.Get("/logs", s.handleGetLogs)
		r.Get("/version", s.handleVersion)
	})

	// Webhook
	r.Post("/webhook", s.handleWebhook)

	// Swagger docs
	r.Get("/docs/swagger.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(docs.SwaggerInfo.ReadDoc()))
	})
	r.Get("/docs/*", httpSwagger.Handler(
		httpSwagger.URL("/docs/swagger.json"),
	))

	// Serve built frontend if --static is set
	if s.cfg.StaticDir != "" {
		if info, err := os.Stat(s.cfg.StaticDir); err == nil && info.IsDir() {
			fileServer := http.FileServer(http.Dir(s.cfg.StaticDir))
			r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/api/") ||
					strings.HasPrefix(r.URL.Path, "/webhook") ||
					strings.HasPrefix(r.URL.Path, "/docs/") ||
					strings.HasPrefix(r.URL.Path, "/ping") {
					http.NotFound(w, r)
					return
				}
				path := filepath.Join(s.cfg.StaticDir, r.URL.Path)
				if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
					http.ServeFile(w, r, filepath.Join(s.cfg.StaticDir, "index.html"))
					return
				}
				fileServer.ServeHTTP(w, r)
			})
		}
	} else {
		r.Get("/", s.handleRoot)
	}

	return r
}

// ListenAndServe starts the server.
func (s *Server) ListenAndServe() error {
	log.Printf("Kestrix server listening on %s", s.cfg.ListenAddr)
	return http.ListenAndServe(s.cfg.ListenAddr, s.router)
}

// ─── API Handlers ─────────────────────────────────────────────

// handleHealth godoc
// @Summary Health check
// @Tags system
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/health [get]
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"service":   "kestrix",
		"pipelines": len(s.webhookLog),
		"webhooks":  len(s.webhookLog),
		"repos":     0,
	})
}

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
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
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
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
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
		http.Error(w, `{"error":"no .tf file found"}`, http.StatusNotFound)
		return
	}

	data, err := os.ReadFile(tfFile)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

// ─── Project Handlers (use FileStore) ─────────────────────────

// parsePagination extracts and validates pagination parameters from query values.
// Returns the parsed limit, offset, or an API error string and HTTP status code.
func parsePagination(q url.Values) (limit int, offset int, errStr string, status int) {
	allowed := map[int]bool{10: true, 25: true, 50: true, 100: true}
	limit = 25
	offset = 0
	status = http.StatusOK
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, 0, `{"error":"invalid limit"}`, http.StatusBadRequest
		}
		limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, 0, `{"error":"invalid offset"}`, http.StatusBadRequest
		}
		offset = n
	}
	if !allowed[limit] {
		return 0, 0, `{"error":"limit must be one of [10,25,50,100]"}`, http.StatusBadRequest
	}
	return
}

func buildPaginationLinks(offset, limit, total int, pathPrefix string) (next, prev *string) {
	end := offset + limit
	if end < total {
		n := fmt.Sprintf("%s?limit=%d&offset=%d", pathPrefix, limit, end)
		next = &n
	}
	if offset > 0 {
		prevOffset := offset - limit
		if prevOffset < 0 {
			prevOffset = 0
		}
		p := fmt.Sprintf("%s?limit=%d&offset=%d", pathPrefix, limit, prevOffset)
		prev = &p
	}
	return
}

type paginationMeta struct {
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
	Next   *string `json:"next"`
	Prev   *string `json:"prev"`
}

// handleListProjects godoc
// @Summary List configured projects
// @Tags projects
// @Produce json
// @Param limit query int false "Items per page (10, 25, 50, 100; default 25)"
// @Param offset query int false "Offset for pagination (default 0)"
// @Success 200 {object} map[string]interface{} "paginated project list"
// @Router /api/projects [get]
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	limit, offset, errStr, status := parsePagination(r.URL.Query())
	if errStr != "" {
		http.Error(w, errStr, status)
		return
	}
	// Load all projects (could be optimized)
	projMap, err := s.store.Load()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	// Convert map to slice for sorting
	list := make([]*store.ProjectConfig, 0, len(projMap))
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
	next, prev := buildPaginationLinks(offset, limit, total, "/api/projects")
	resp := map[string]interface{}{
		"items":      paged,
		"pagination": paginationMeta{Total: total, Limit: limit, Offset: offset, Next: next, Prev: prev},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
	return
}

// handleCreateProject godoc
// @Summary Create a project (point at a repo and its pipeline definition directory)
// @Tags projects
// @Accept json
// @Produce json
// @Param request body store.ProjectConfig true "Project config"
// @Success 201 {object} store.ProjectConfig
// @Router /api/projects [post]
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req store.ProjectConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	if req.RepoPath == "" && req.RepoURL == "" {
		http.Error(w, `{"error":"repo_path or repo_url is required"}`, http.StatusBadRequest)
		return
	}

	if err := s.store.Upsert(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
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
// @Success 200 {object} store.ProjectConfig
// @Router /api/projects/{name} [get]
func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	p, err := s.store.Get(name)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if p == nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
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
// @Param request body store.ProjectConfig true "Updated config"
// @Success 200 {object} store.ProjectConfig
// @Router /api/projects/{name} [put]
func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	existing, err := s.store.Get(name)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if existing == nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
		return
	}
	var update store.ProjectConfig
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	update.Name = name
	if update.RepoPath == "" {
		update.RepoPath = existing.RepoPath
	}
	if update.PipelineDir == "" {
		update.PipelineDir = existing.PipelineDir
	}
	if update.PipelineFile == "" {
		update.PipelineFile = existing.PipelineFile
	}
	if update.CreatedAt == "" {
		update.CreatedAt = existing.CreatedAt
	}
	if err := s.store.Upsert(&update); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
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
	if err := s.store.Delete(name); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "deleted"})
}

// projectPipelineDir resolves the directory containing a project's pipeline
// definitions and prevents a relative configuration from escaping RepoPath.
func (s *Server) projectPipelineDir(p *store.ProjectConfig) (string, error) {
	pipelineDir := p.PipelineDir
	if pipelineDir == "" {
		pipelineDir = filepath.Dir(p.PipelineFile)
		if pipelineDir == "" {
			pipelineDir = "."
		}
	}
	repoPath, err := filepath.Abs(p.RepoPath)
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
func (s *Server) projectPipelinePath(p *store.ProjectConfig) (string, error) {
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
func (s *Server) parseProjectPipeline(p *store.ProjectConfig) (model.Project, error) {
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
	p, err := s.store.Get(name)
	if err != nil || p == nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
		return
	}
	path, err := s.projectPipelinePath(p)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

// handleGetProjectDefinition returns the parsed configured pipeline file.
func (s *Server) handleGetProjectDefinition(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	p, err := s.store.Get(name)
	if err != nil || p == nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
		return
	}
	proj, err := s.parseProjectPipeline(p)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
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
	p, err := s.store.Get(name)
	if err != nil || p == nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	pipelineDir, err := s.projectPipelineDir(p)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	path, err := s.projectPipelinePath(p)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
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
	p, err := s.store.Get(name)
	if err != nil || p == nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
		return
	}
	var req struct {
		Changed []string `json:"changed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	proj, err := s.parseProjectPipeline(p)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}
	changedSet := make(map[string]bool)
	for _, c := range req.Changed {
		changedSet[c] = true
	}
	e := dag.New(proj)
	nodes, err := e.Resolve(changedSet)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
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

// handleRunProject executes a registered project's selected tasks in its local
// checkout. This endpoint intentionally accepts only a project name, rather
// than a path or command, so execution stays limited to registered projects
// and the scripts committed in their pipeline definition.
func (s *Server) handleRunProject(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	p, err := s.store.Get(name)
	if err != nil || p == nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
		return
	}
	var req struct {
		Changed       []string `json:"changed"`
		IncludeManual bool     `json:"include_manual"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	proj, err := s.parseProjectPipeline(p)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}
	changedSet := make(map[string]bool, len(proj.Repos))
	if len(req.Changed) == 0 {
		for _, repo := range proj.Repos {
			changedSet[repo.Name] = true
		}
	} else {
		for _, repo := range proj.Repos {
			changedSet[repo.Name] = false
		}
		for _, changed := range req.Changed {
			changedSet[changed] = true
		}
	}

	// Build variable store from project variables and inject env vars.
	varStore := variables.New()
	for _, v := range proj.Variables {
		val := v.Value
		if val == "" {
			val = v.Default
		}
		opts := []variables.Option{}
		if v.Protected {
			opts = append(opts, variables.WithProtected(true))
		}
		if v.Masked {
			opts = append(opts, variables.WithMasked(true))
		}
		switch v.Scope {
		case "repo":
			varStore.SetRepo(v.Repo, v.Key, val, opts...)
		case "job":
			varStore.SetJob(v.Repo, v.Job, v.Key, val, opts...)
		default:
			varStore.SetProject(v.Key, val, opts...)
		}
	}
	// Also pull in METACI_* env vars.
	envVars := variables.FromEnv("METACI_")
	for _, v := range envVars.List() {
		varStore.SetProject(v.Key, v.Value)
	}
	// Inject Docker config as env vars.
	if s.cfg.Docker.Registry != "" {
		varStore.SetProject("DOCKER_REGISTRY", s.cfg.Docker.Registry)
	}
	if s.cfg.Docker.Image != "" {
		varStore.SetProject("DOCKER_IMAGE", s.cfg.Docker.Image)
	}
	if s.cfg.Docker.Username != "" {
		varStore.SetProject("DOCKER_USERNAME", s.cfg.Docker.Username)
	}
	if s.cfg.Docker.Password != "" {
		varStore.SetProject("DOCKER_PASSWORD", s.cfg.Docker.Password)
	}

	// Persist run start if PG store is available.
	var runID int
	if pgStore, ok := s.store.(*store.PGStore); ok {
		snap := store.SnapshotFromProject(&proj)
		var runErr error
		runID, runErr = pgStore.CreateRun(name, req.Changed, req.IncludeManual, snap)
		if runErr != nil {
			log.Printf("warning: could not create run record: %v", runErr)
		}
		// Pre-populate all tasks as pending so the DAG shows them gray from the start.
		if runErr == nil {
			for _, t := range proj.Tasks {
				pgStore.AppendTaskResult(runID, store.TaskResult{
					Name:   t.Name,
					Status: "pending",
				})
			}
		}
	}

	// Return immediately so the frontend doesn't timeout.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"project": proj.Name,
		"run_id":  runID,
		"status":  "running",
		"changed": req.Changed,
	})

	// Execute pipeline in background.
	go func() {
		var bgOutput bytes.Buffer
		var taskResults []runner.TaskResult
		var err error

		// Append each task result to PG as it completes so the FE can poll progress.
		pgStore, _ := s.store.(*store.PGStore)
		onStart := func(name string) {
			if pgStore != nil && runID > 0 {
				pgStore.AppendTaskResult(runID, store.TaskResult{
					Name:   name,
					Status: "running",
				})
			}
		}
		onComplete := func(tr runner.TaskResult) {
			if pgStore != nil && runID > 0 {
				pgStore.AppendTaskResult(runID, store.TaskResult{
					Name: tr.Name, Status: tr.Status, Output: tr.Output,
					Error: tr.Error, Duration: tr.Duration,
				})
				// Flush aggregate output so the FE can show logs during execution.
				pgStore.FlushRunOutput(runID, bgOutput.String())
			}
		}

		opts := runner.Options{
			WorkDir:        p.RepoPath,
			IncludeManual:  req.IncludeManual,
			Variables:      varStore,
			Stdout:         &bgOutput,
			Stderr:         &bgOutput,
			OnTaskStart:    onStart,
			OnTaskComplete: onComplete,
		}

		if s.cfg.Runner.Mode == "gitlab-runner" {
			nodes, resErr := dag.New(proj).Resolve(changedSet)
			if resErr != nil {
				err = resErr
			} else {
				taskResults, err = runner.RunGitLabRunner(context.Background(), proj, nodes, opts, GenerateGitLabCI)
			}
		} else if s.cfg.Runner.Mode == "local" {
			taskResults, err = runner.RunLocal(context.Background(), proj, changedSet, opts)
		} else {
			taskResults, err = runner.Run(context.Background(), proj, changedSet, opts)
		}

		status := "passed"
		errMsg := ""
		if err != nil {
			status = "failed"
			errMsg = err.Error()
		}
		// Also check task results — runner may not return error if it continues past failures.
		hasManual := false
		for _, tr := range taskResults {
			if tr.Status == "failed" {
				status = "failed"
				if errMsg == "" {
					errMsg = tr.Error
				}
				break
			}
			if tr.Status == "manual" {
				hasManual = true
			}
		}
		if status == "passed" && hasManual {
			status = "manual"
		}
		if pgStore != nil && runID > 0 {
			var trs []store.TaskResult
			for _, tr := range taskResults {
				trs = append(trs, store.TaskResult{
					Name: tr.Name, Status: tr.Status, Output: tr.Output,
					Error: tr.Error, Duration: tr.Duration,
				})
			}
			pgStore.FinishRun(runID, status, bgOutput.String(), errMsg, trs)
			log.Printf("run #%d finished: %s", runID, status)
		}
	}()
}

// handleRunTask executes a single task by name.
func (s *Server) handleRunTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	taskName := chi.URLParam(r, "task")
	p, err := s.store.Get(name)
	if err != nil || p == nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
		return
	}
	proj, err := s.parseProjectPipeline(p)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
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
		http.Error(w, fmt.Sprintf(`{"error":"task %q not found"}`, taskName), http.StatusNotFound)
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

	// Build variable store.
	varStore := variables.New()
	for _, v := range proj.Variables {
		val := v.Value
		if val == "" {
			val = v.Default
		}
		opts := []variables.Option{}
		if v.Protected {
			opts = append(opts, variables.WithProtected(true))
		}
		if v.Masked {
			opts = append(opts, variables.WithMasked(true))
		}
		switch v.Scope {
		case "repo":
			varStore.SetRepo(v.Repo, v.Key, val, opts...)
		case "job":
			varStore.SetJob(v.Repo, v.Job, v.Key, val, opts...)
		default:
			varStore.SetProject(v.Key, val, opts...)
		}
	}
	envVars := variables.FromEnv("METACI_")
	for _, v := range envVars.List() {
		varStore.SetProject(v.Key, v.Value)
	}
	if s.cfg.Docker.Registry != "" {
		varStore.SetProject("DOCKER_REGISTRY", s.cfg.Docker.Registry)
	}
	if s.cfg.Docker.Image != "" {
		varStore.SetProject("DOCKER_IMAGE", s.cfg.Docker.Image)
	}
	if s.cfg.Docker.Username != "" {
		varStore.SetProject("DOCKER_USERNAME", s.cfg.Docker.Username)
	}
	if s.cfg.Docker.Password != "" {
		varStore.SetProject("DOCKER_PASSWORD", s.cfg.Docker.Password)
	}

	// Persist run.
	var runID int
	if pgStore, ok := s.store.(*store.PGStore); ok {
		snap := store.SnapshotFromProject(&proj)
		var runErr error
		runID, runErr = pgStore.CreateRun(name, []string{taskName}, false, snap)
		if runErr != nil {
			log.Printf("warning: could not create run record: %v", runErr)
		}
	}

	var output bytes.Buffer
	var taskResults []runner.TaskResult
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
	if pgStore, ok := s.store.(*store.PGStore); ok && runID > 0 {
		var trs []store.TaskResult
		for _, tr := range taskResults {
			trs = append(trs, store.TaskResult{
				Name: tr.Name, Status: tr.Status, Output: tr.Output,
				Error: tr.Error, Duration: tr.Duration,
			})
		}
		pgStore.FinishRun(runID, status, output.String(), errMsg, trs)
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
	pgStore, ok := s.store.(*store.PGStore)
	if !ok {
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}
	limit, offset, errStr, status := parsePagination(r.URL.Query())
	if errStr != "" {
		http.Error(w, errStr, status)
		return
	}
	total, err := pgStore.CountRuns(name)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	runs, err := pgStore.ListRuns(name, limit, offset)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []store.Run{}
	}
	pathPrefix := fmt.Sprintf("/api/projects/%s/runs", name)
	next, prev := buildPaginationLinks(offset, limit, total, pathPrefix)
	resp := map[string]interface{}{
		"items":      runs,
		"pagination": paginationMeta{Total: total, Limit: limit, Offset: offset, Next: next, Prev: prev},
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
	pgStore, ok := s.store.(*store.PGStore)
	if !ok {
		http.Error(w, `{"error":"runs not supported"}`, http.StatusBadRequest)
		return
	}
	runID := 0
	fmt.Sscanf(chi.URLParam(r, "runID"), "%d", &runID)
	if runID == 0 {
		http.Error(w, `{"error":"invalid run ID"}`, http.StatusBadRequest)
		return
	}
	run, err := pgStore.GetRun(runID)
	if err != nil || run == nil {
		http.Error(w, `{"error":"run not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

// handleApproveManualTask runs a manual task that is waiting for approval on a run.
func (s *Server) handleApproveManualTask(w http.ResponseWriter, r *http.Request) {
	pgStore, ok := s.store.(*store.PGStore)
	if !ok {
		http.Error(w, `{"error":"runs not supported"}`, http.StatusBadRequest)
		return
	}
	name := chi.URLParam(r, "name")
	taskName := chi.URLParam(r, "task")
	runID := 0
	fmt.Sscanf(chi.URLParam(r, "runID"), "%d", &runID)
	if runID == 0 {
		http.Error(w, `{"error":"invalid run ID"}`, http.StatusBadRequest)
		return
	}

	run, err := pgStore.GetRun(runID)
	if err != nil || run == nil || run.ProjectName != name {
		http.Error(w, `{"error":"run not found"}`, http.StatusNotFound)
		return
	}

	// Latest status per task.
	latest := make(map[string]store.TaskResult)
	for _, tr := range run.TaskResults {
		latest[tr.Name] = tr
	}
	cur, ok := latest[taskName]
	if !ok {
		http.Error(w, `{"error":"task not in run"}`, http.StatusNotFound)
		return
	}
	// Allow approve for "manual", or legacy "skipped" manual jobs (no dependency error).
	isWaiting := cur.Status == "manual" ||
		(cur.Status == "skipped" && cur.Error != "dependency failed" && cur.Error != "dependency canceled")
	if !isWaiting {
		http.Error(w, fmt.Sprintf(`{"error":"task %q is not waiting for manual approval (status=%s)"}`, taskName, cur.Status), http.StatusBadRequest)
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
	p, err := s.store.Get(name)
	if err != nil || p == nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
		return
	}
	proj, err := s.parseProjectPipeline(p)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
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
		http.Error(w, fmt.Sprintf(`{"error":"task %q not found in pipeline"}`, taskName), http.StatusNotFound)
		return
	}
	if !manualOK {
		http.Error(w, `{"error":"task is not manual"}`, http.StatusBadRequest)
		return
	}
	for _, dep := range dependsOn {
		d, exists := latest[dep]
		if !exists || d.Status != "passed" {
			http.Error(w, fmt.Sprintf(`{"error":"dependency %q has not passed"}`, dep), http.StatusBadRequest)
			return
		}
	}

	// Build variables (same as full run).
	varStore := variables.New()
	for _, v := range proj.Variables {
		val := v.Value
		if val == "" {
			val = v.Default
		}
		opts := []variables.Option{}
		if v.Protected {
			opts = append(opts, variables.WithProtected(true))
		}
		if v.Masked {
			opts = append(opts, variables.WithMasked(true))
		}
		switch v.Scope {
		case "repo":
			varStore.SetRepo(v.Repo, v.Key, val, opts...)
		case "job":
			varStore.SetJob(v.Repo, v.Job, v.Key, val, opts...)
		default:
			varStore.SetProject(v.Key, val, opts...)
		}
	}
	for _, v := range variables.FromEnv("METACI_").List() {
		varStore.SetProject(v.Key, v.Value)
	}
	if s.cfg.Docker.Registry != "" {
		varStore.SetProject("DOCKER_REGISTRY", s.cfg.Docker.Registry)
	}
	if s.cfg.Docker.Image != "" {
		varStore.SetProject("DOCKER_IMAGE", s.cfg.Docker.Image)
	}
	if s.cfg.Docker.Username != "" {
		varStore.SetProject("DOCKER_USERNAME", s.cfg.Docker.Username)
	}
	if s.cfg.Docker.Password != "" {
		varStore.SetProject("DOCKER_PASSWORD", s.cfg.Docker.Password)
	}

	_ = pgStore.ApplyTaskResult(runID, store.TaskResult{Name: taskName, Status: "running"}, "")

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
		st := store.TaskResult{
			Name: tr.Name, Status: tr.Status, Output: tr.Output,
			Error: tr.Error, Duration: tr.Duration,
		}
		if runErr != nil && st.Status == "" {
			st.Status = "failed"
			st.Error = runErr.Error()
		}
		if err := pgStore.ApplyTaskResult(runID, st, output.String()); err != nil {
			log.Printf("approve task %s on run #%d: %v", taskName, runID, err)
		}
	}()
}

// handleCancelRun cancels a running pipeline run.
func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	pgStore, ok := s.store.(*store.PGStore)
	if !ok {
		http.Error(w, `{"error":"runs not supported"}`, http.StatusBadRequest)
		return
	}
	runID := 0
	fmt.Sscanf(chi.URLParam(r, "runID"), "%d", &runID)
	if runID == 0 {
		http.Error(w, `{"error":"invalid run ID"}`, http.StatusBadRequest)
		return
	}
	if err := pgStore.CancelRun(runID); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
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
	pgStore, ok := s.store.(*store.PGStore)
	if !ok {
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}
	runs, err := pgStore.ListAllRuns(100)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []store.Run{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

// ─── Pipeline Handlers (global, use --dir) ────────────────────

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
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if req.Dir == "" {
		req.Dir = s.cfg.PipelineDir
	}

	proj, err := parser.ParseDir(req.Dir)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}

	changedSet := make(map[string]bool)
	for _, c := range req.Changed {
		changedSet[c] = true
	}

	e := dag.New(proj)
	nodes, err := e.Resolve(changedSet)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
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
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if req.Dir == "" {
		req.Dir = s.cfg.PipelineDir
	}

	proj, err := parser.ParseDir(req.Dir)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}

	e := dag.New(proj)
	changedSet := make(map[string]bool)
	for _, c := range req.Changed {
		changedSet[c] = true
	}

	nodes, err := e.Resolve(changedSet)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
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
		http.Error(w, fmt.Sprintf(`{"valid":false,"error":%q}`, err.Error()), http.StatusBadRequest)
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

// handleGetLogs godoc
// @Summary Get webhook logs
// @Tags webhooks
// @Produce json
// @Success 200 {array} map[string]interface{}
// @Router /api/logs [get]
func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.webhookLog)
}

// handleWebhook godoc
// @Summary Receive GitLab webhook
// @Tags webhooks
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /webhook [post]
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Signature.Enabled && s.cfg.Signature.Secret != "" {
		token := r.Header.Get("X-Gitlab-Token")
		if token != s.cfg.Signature.Secret {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	event, err := ParseGitLabPushEvent(body)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}
	if event == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ignored", "reason": "not a push event"})
		return
	}

	logEntry := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"repo":      event.Repo,
		"branch":    event.Branch,
		"commits":   event.CommitCount,
		"tasks":     []string{},
		"status":    "received",
	}

	// ─── Look up project by GitLab project ID ────────────────
	var pipelineDir string
	if event.GitLabProjectID > 0 && s.store != nil {
		projCfg, err := s.store.FindByGitLabID(event.GitLabProjectID)
		if err != nil {
			log.Printf("webhook: store lookup error: %v", err)
			logEntry["status"] = "error"
			logEntry["error"] = err.Error()
			s.webhookLog = append(s.webhookLog, logEntry)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(logEntry)
			return
		}
		if projCfg == nil {
			log.Printf("webhook: unknown project gitlab_id=%d", event.GitLabProjectID)
			logEntry["status"] = "ignored"
			logEntry["error"] = "unknown project"
			s.webhookLog = append(s.webhookLog, logEntry)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "ignored", "reason": "unknown project",
			})
			return
		}
		pipelineDir = filepath.Join(projCfg.RepoPath, projCfg.PipelineDir)
	} else {
		pipelineDir = s.cfg.PipelineDir
	}

	proj, err := parser.ParseDir(pipelineDir)
	if err != nil {
		log.Printf("webhook: parse error: %v", err)
		logEntry["status"] = "error"
		logEntry["error"] = err.Error()
		s.webhookLog = append(s.webhookLog, logEntry)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(logEntry)
		return
	}

	classifier := DefaultClassifier()
	changedRepos := classifier.MatchRepos(event.FilesChanged, proj.Repos)

	changedSet := make(map[string]bool)
	for _, c := range changedRepos {
		changedSet[c] = true
	}

	e := dag.New(proj)
	nodes, _ := e.Resolve(changedSet)

	var triggeredTasks []string
	for _, n := range nodes {
		if n.Triggered {
			triggeredTasks = append(triggeredTasks, n.Task.Name)
		}
	}

	logEntry["tasks"] = triggeredTasks
	logEntry["status"] = "triggered"
	if len(triggeredTasks) == 0 {
		logEntry["status"] = "skipped"
	}

	log.Printf("webhook: repo=%s branch=%s commits=%d files=%d tasks=%v",
		event.Repo, event.Branch, event.CommitCount, len(changedRepos), triggeredTasks)

	// ─── Dispatch to GitLab (if configured) ──────────────────
	var dispatchResult map[string]interface{}
	dispatchID := s.cfg.Dispatch.ProjectID
	if event.GitLabProjectID > 0 && s.store != nil {
		if projCfg, _ := s.store.FindByGitLabID(event.GitLabProjectID); projCfg != nil && projCfg.GitLabProjectID > 0 {
			dispatchID = fmt.Sprintf("%d", projCfg.GitLabProjectID)
		}
	}
	if s.cfg.Dispatch.Enabled && dispatchID != "" && len(triggeredTasks) > 0 {
		var projectID int
		fmt.Sscanf(dispatchID, "%d", &projectID)
		if projectID > 0 {
			client := &dispatch.Client{
				BaseURL: s.cfg.Dispatch.GitLabURL,
				Token:   s.cfg.Dispatch.GitLabToken,
			}
			vars := map[string]string{
				"METACI_TRIGGERED_TASKS": strings.Join(triggeredTasks, ","),
				"METACI_CHANGED_REPOS":   strings.Join(changedRepos, ","),
				"METACI_DISPATCH_TIME":   time.Now().UTC().Format(time.RFC3339),
			}
			ref := s.cfg.Branch
			if ref == "" {
				ref = event.Branch
			}

			resp, err := client.Dispatch(dispatch.DispatchRequest{
				ProjectID: projectID,
				Ref:       ref,
				Variables: vars,
			})
			if err != nil {
				log.Printf("webhook: dispatch error: %v", err)
				logEntry["dispatch"] = map[string]interface{}{
					"dispatched": false,
					"error":      err.Error(),
				}
			} else {
				log.Printf("webhook: dispatched pipeline %d (%s)", resp.PipelineID, resp.WebURL)
				logEntry["dispatch"] = map[string]interface{}{
					"dispatched":   true,
					"pipeline_id":  resp.PipelineID,
					"pipeline_url": resp.WebURL,
				}
				dispatchResult = logEntry["dispatch"].(map[string]interface{})
			}
		}
	}

	s.webhookLog = append(s.webhookLog, logEntry)

	response := map[string]interface{}{
		"status":  "ok",
		"project": proj.Name,
		"changed": changedRepos,
		"tasks":   triggeredTasks,
		"count":   len(triggeredTasks),
	}
	if dispatchResult != nil {
		response["dispatched"] = dispatchResult["dispatched"]
		response["pipeline_url"] = dispatchResult["pipeline_url"]
		response["pipeline_id"] = dispatchResult["pipeline_id"]
	} else {
		response["dispatched"] = false
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Kestrix</title></head>
<body style="font-family:monospace;background:#0a1929;color:#e0e0e0;padding:40px">
<h1>🚀 Kestrix — CI/CD Control Plane</h1>
<p>Version %s — API endpoints:</p>
<ul>
  <li><a href="/docs/">/docs/</a> — Swagger UI</li>
  <li><a href="/health">/health</a> — Health check</li>
  <li><a href="/api/pipeline">/api/pipeline</a> — Get pipeline definition</li>
  <li><a href="/api/validate">/api/validate</a> — Validate pipeline</li>
  <li><a href="/api/logs">/api/logs</a> — Webhook logs</li>
</ul>
<p>Frontend: <code>cd ui && npm run dev</code> (port 3000)</p>
</body>
</html>`, version.Version)
}

// handleVersion returns the server version
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"version": version.Version})
}
