package web

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"gitlab.com/lunaris-bot/metaci/internal/dag"
	"gitlab.com/lunaris-bot/metaci/internal/model"
	"gitlab.com/lunaris-bot/metaci/internal/parser"
)

// Config holds the server configuration.
type Config struct {
	// ListenAddr is the address to bind the HTTP server to (e.g. ":8080").
	ListenAddr string

	// PipelineDir is the directory containing the pipeline .tf files.
	PipelineDir string
}

// Server is the MetaCI webhook receiver. It listens for GitLab push events,
// evaluates the DAG against the changed files, and dispatches tasks.
type Server struct {
	cfg        Config
	classifier *FileClassifier
}

// NewServer creates a new webhook server.
func NewServer(cfg Config) *Server {
	return &Server{
		cfg:        cfg,
		classifier: DefaultClassifier(),
	}
}

// Router returns the Chi router with all routes registered.
func (s *Server) Router() *chi.Mux {
	r := chi.NewRouter()

	// Middleware stack.
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.Heartbeat("/ping"))

	// Routes.
	r.Get("/health", s.handleHealth)
	r.Post("/webhook", s.handleWebhook)
	r.Get("/", s.handleRoot)

	return r
}

// Run starts the HTTP server and blocks until it returns an error.
func (s *Server) Run() error {
	router := s.Router()
	log.Printf("MetaCI webhook server listening on %s", s.cfg.ListenAddr)
	log.Printf("Pipeline dir: %s", s.cfg.PipelineDir)
	return http.ListenAndServe(s.cfg.ListenAddr, router)
}

// --- Handlers ---

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name":    "MetaCI",
		"version": "0.1.0",
		"status":  "running",
		"endpoints": map[string]string{
			"webhook": "POST /webhook",
			"health":  "GET /health",
			"ping":    "GET /ping",
		},
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleWebhook is the main webhook handler.
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("read body: %v", err))
		return
	}
	defer r.Body.Close()

	// Step 1: Parse the webhook payload.
	event, err := ParseGitLabPushEvent(body)
	if err != nil {
		log.Printf("webhook parse error: %v", err)
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("parse error: %v", err))
		return
	}

	log.Printf("webhook: repo=%s branch=%s commits=%d files=%d",
		event.Repo, event.Branch, event.CommitCount, len(event.FilesChanged))

	if event.IsEmpty() {
		log.Printf("no file changes, skipping")
		s.writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "skipped",
			"reason": "no file changes in push",
		})
		return
	}

	// Step 2: Load the pipeline definition.
	proj, err := parser.ParseDir(s.cfg.PipelineDir)
	if err != nil {
		log.Printf("pipeline parse error: %v", err)
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("pipeline parse error: %v", err))
		return
	}

	// Step 3: Classify changes.
	changedRepos := s.classifyChanges(event, proj)

	// Step 4: Evaluate DAG.
	engine := dag.New(proj)
	nodes, err := engine.Resolve(changedRepos)
	if err != nil {
		log.Printf("DAG resolve error: %v", err)
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("DAG error: %v", err))
		return
	}

	log.Printf("resolved %d tasks to execute", len(nodes))

	// Step 5: Generate gitlab-ci.yml.
	yamlContent := GenerateGitLabCI(proj, nodes)

	// Step 6: Return the result.
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":        "ok",
		"project":       proj.Name,
		"repo":          event.Repo,
		"branch":        event.Branch,
		"changed_repos": changedRepos,
		"tasks":         nodesToJSON(nodes),
		"gitlab_ci":     yamlContent,
	})
}

// --- Helpers ---

func (s *Server) classifyChanges(event *PushEvent, proj model.Project) map[string]bool {
	repoPaths := make(map[string]string)
	for _, r := range proj.Repos {
		repoPaths[r.Name] = r.Path
	}

	changed := make(map[string]bool)
	for name, path := range repoPaths {
		if path == event.Repo || event.Repo == path {
			changed[name] = s.classifier.HasCodeChanges(event.FilesChanged)
		} else {
			changed[name] = false
		}
	}

	if len(changed) == 0 {
		log.Printf("warning: repo %q not found in pipeline project %q", event.Repo, proj.Name)
	}

	return changed
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	log.Printf("error %d: %s", status, msg)
	s.writeJSON(w, status, map[string]string{"error": msg})
}

func nodesToJSON(nodes []dag.Node) []map[string]interface{} {
	var out []map[string]interface{}
	for _, n := range nodes {
		out = append(out, map[string]interface{}{
			"name":       n.Task.Name,
			"triggered":  n.Triggered,
			"reason":     n.TriggeredReason,
			"repo":       n.Task.Execute.Repo,
			"script":     n.Task.Execute.Script,
			"image":      n.Task.Execute.Image,
			"manual":     n.Task.Manual,
			"depends_on": n.Task.DependsOn,
		})
	}
	return out
}
