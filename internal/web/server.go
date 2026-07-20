package web

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gitlab.com/lunaris-bot/metaci/internal/dag"
	"gitlab.com/lunaris-bot/metaci/internal/dispatch"
	"gitlab.com/lunaris-bot/metaci/internal/parser"
	"gitlab.com/lunaris-bot/metaci/internal/store"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"
	_ "gitlab.com/lunaris-bot/metaci/docs"
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
	Branch      string
	Signature   SignatureConfig
	Dispatch    DispatchConfig
}

// Server is the HTTP server.
type Server struct {
	cfg        Config
	router     chi.Router
	webhookLog []map[string]interface{}
	store      *store.FileStore
}

// NewServer creates a new HTTP server.
func NewServer(cfg Config) *Server {
	s := &Server{cfg: cfg}

	// Initialize persistent project store.
	storePath := store.DefaultPath()
	fs, err := store.New(storePath)
	if err != nil {
		log.Printf("warning: could not init project store at %s: %v", storePath, err)
		// Fall back to temp dir with PID to avoid cross-instance conflicts.
		fallback := filepath.Join(os.TempDir(), fmt.Sprintf("metaci-%d-projects.json", os.Getpid()))
		fs, _ = store.New(fallback)
	}
	s.store = fs

	// Auto-load all .tf files as separate projects.
	if cfg.PipelineDir != "" {
		entries, err := os.ReadDir(cfg.PipelineDir)
		if err == nil {
			absPath, _ := filepath.Abs(cfg.PipelineDir)
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".tf") {
					continue
				}
				filePath := filepath.Join(cfg.PipelineDir, e.Name())
				proj, parseErr := parser.ParseFile(filePath)
				if parseErr != nil || proj.Name == "" {
					continue
				}
				existing, _ := s.store.Get(proj.Name)
				if existing == nil {
					s.store.Upsert(&store.ProjectConfig{
						Name:         proj.Name,
						RepoPath:     absPath,
						PipelineFile: e.Name(),
					})
				}
			}
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
		r.Delete("/projects/{name}", s.handleDeleteProject)
		r.Get("/projects/{name}/pipeline", s.handleGetProjectPipeline)
		r.Post("/projects/{name}/pipeline", s.handleSaveProjectPipeline)
		r.Post("/projects/{name}/resolve", s.handleResolveProject)
		r.Post("/resolve", s.handleResolve)
		r.Post("/generate", s.handleGenerate)
		r.Get("/validate", s.handleValidate)
		r.Get("/logs", s.handleGetLogs)
	})

	// Webhook
	r.Post("/webhook", s.handleWebhook)

	// Swagger docs
	r.Get("/docs/*", httpSwagger.Handler(
		httpSwagger.URL("/docs/swagger.json"),
	))

	// Serve built frontend (ui/dist/) if it exists
	uiDist := filepath.Join(s.cfg.PipelineDir, "..", "ui", "dist")
	if info, err := os.Stat(uiDist); err == nil && info.IsDir() {
		fileServer := http.FileServer(http.Dir(uiDist))
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") ||
				strings.HasPrefix(r.URL.Path, "/webhook") ||
				strings.HasPrefix(r.URL.Path, "/docs/") ||
				strings.HasPrefix(r.URL.Path, "/ping") {
				http.NotFound(w, r)
				return
			}
			path := filepath.Join(uiDist, r.URL.Path)
			if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
				http.ServeFile(w, r, filepath.Join(uiDist, "index.html"))
				return
			}
			fileServer.ServeHTTP(w, r)
		})
	} else {
		r.Get("/", s.handleRoot)
	}

	return r
}

// ListenAndServe starts the server.
func (s *Server) ListenAndServe() error {
	log.Printf("MetaCI server listening on %s", s.cfg.ListenAddr)
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
		"service":   "metaci",
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

// handleListProjects godoc
// @Summary List configured projects
// @Tags projects
// @Produce json
// @Success 200 {array} store.ProjectConfig
// @Router /api/projects [get]
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.store.Load()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	list := make([]*store.ProjectConfig, 0, len(projects))
	for _, p := range projects {
		list = append(list, p)
	}
	// Sort by name for deterministic output.
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// handleCreateProject godoc
// @Summary Create a project (point at a repo containing pipeline.tf)
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

// projectPipelinePath resolves the absolute path to a project's pipeline file.
func (s *Server) projectPipelinePath(p *store.ProjectConfig) string {
	if p.PipelineFile == "" {
		p.PipelineFile = "pipeline.tf"
	}
	return filepath.Join(p.RepoPath, p.PipelineFile)
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
	data, err := os.ReadFile(s.projectPipelinePath(p))
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

// handleSaveProjectPipeline godoc
// @Summary Save pipeline HCL for a project (writes pipeline.tf to repo path)
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
	if err := os.MkdirAll(p.RepoPath, 0o755); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	path := s.projectPipelinePath(p)
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
	proj, err := parser.ParseDir(p.RepoPath)
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

	proj, err := parser.ParseDir(s.cfg.PipelineDir)
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
	if s.cfg.Dispatch.Enabled && s.cfg.Dispatch.ProjectID != "" && len(triggeredTasks) > 0 {
		var projectID int
		fmt.Sscanf(s.cfg.Dispatch.ProjectID, "%d", &projectID)
		if projectID > 0 {
			client := &dispatch.Client{
				BaseURL: s.cfg.Dispatch.GitLabURL,
				Token:   s.cfg.Dispatch.GitLabToken,
			}
			// Build variables for the pipeline.
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
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>MetaCI</title></head>
<body style="font-family:monospace;background:#0a1929;color:#e0e0e0;padding:40px">
<h1>🚀 MetaCI — CI/CD Control Plane</h1>
<p>PoC v0.2 — API endpoints:</p>
<ul>
  <li><a href="/docs/">/docs/</a> — Swagger UI</li>
  <li><a href="/health">/health</a> — Health check</li>
  <li><a href="/api/pipeline">/api/pipeline</a> — Get pipeline definition</li>
  <li><a href="/api/validate">/api/validate</a> — Validate pipeline</li>
  <li><a href="/api/logs">/api/logs</a> — Webhook logs</li>
</ul>
<p>Frontend: <code>cd ui && npm run dev</code> (port 3000)</p>
</body>
</html>`)
}
