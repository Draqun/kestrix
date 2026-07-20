package web

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"

	"gitlab.com/lunaris-bot/metaci/internal/dag"
	"gitlab.com/lunaris-bot/metaci/internal/parser"
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
}

// NewServer creates a new HTTP server.
func NewServer(cfg Config) *Server {
	s := &Server{cfg: cfg}
	s.router = s.buildRouter()
	return s
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()

	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Heartbeat("/ping"))

	// API routes
	r.Route("/api", func(r chi.Router) {
		r.Get("/health", s.handleHealth)
		r.Get("/pipeline", s.handleGetPipeline)
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

	s.webhookLog = append(s.webhookLog, logEntry)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"project": proj.Name,
		"changed": changedRepos,
		"tasks":   triggeredTasks,
		"count":   len(triggeredTasks),
	})
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
