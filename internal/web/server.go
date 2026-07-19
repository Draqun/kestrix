package web

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"

	"gitlab.com/lunaris-bot/metaci/internal/dag"
	"gitlab.com/lunaris-bot/metaci/internal/dispatch"
	"gitlab.com/lunaris-bot/metaci/internal/model"
	"gitlab.com/lunaris-bot/metaci/internal/parser"
	"gitlab.com/lunaris-bot/metaci/internal/variables"
)

// Config holds the server configuration.
type Config struct {
	ListenAddr        string
	PipelineDir       string
	Signature         SignatureConfig
	Dispatch          *dispatch.Client
	DispatchProjectID int
}

// Server is the MetaCI webhook receiver.
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

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	// Optional webhook signature verification.
	if mw, err := s.cfg.Signature.Middleware(); err != nil {
		log.Printf("WARNING: webhook signature config error: %v", err)
	} else if mw != nil {
		log.Printf("Webhook signature verification enabled (method: %s)", s.cfg.Signature.Method)
		r.Use(mw)
	}

	// Routes.
	r.Get("/health", s.handleHealth)
	r.Post("/webhook", s.handleWebhook)
	r.Get("/", s.handleRoot)

	// Swagger docs — available at /docs/
	r.Get("/docs/*", httpSwagger.Handler(
		httpSwagger.URL("/docs/swagger.json"),
	))
	r.Handle("/docs/swagger.json", http.FileServer(http.Dir("docs")))
	r.Handle("/docs/swagger.yaml", http.FileServer(http.Dir("docs")))

	return r
}

// Run starts the HTTP server.
func (s *Server) Run() error {
	router := s.Router()
	log.Printf("MetaCI webhook server listening on %s", s.cfg.ListenAddr)
	log.Printf("Pipeline dir: %s", s.cfg.PipelineDir)
	if s.cfg.Dispatch != nil {
		log.Printf("Dispatch enabled → project %d", s.cfg.DispatchProjectID)
	}
	return http.ListenAndServe(s.cfg.ListenAddr, router)
}

// --- Handlers ---

// handleRoot godoc
// @Summary      MetaCI API root
// @Description  Returns MetaCI server information and available endpoints
// @Tags         info
// @Produce      json
// @Success      200 {object} map[string]interface{}
// @Router       / [get]
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name":    "MetaCI",
		"version": "0.2.0",
		"status":  "running",
		"endpoints": map[string]string{
			"webhook": "POST /webhook",
			"health":  "GET /health",
			"docs":    "GET /docs",
		},
	})
}

// handleHealth godoc
// @Summary      Health check
// @Description  Returns server health status
// @Tags         health
// @Produce      json
// @Success      200 {object} map[string]string
// @Router       /health [get]
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleWebhook godoc
// @Summary      Process GitLab push webhook
// @Description  Receives a GitLab push event, evaluates the pipeline DAG against changed files, and optionally dispatches to GitLab runners
// @Tags         webhook
// @Accept       json
// @Produce      json
// @Param        X-Gitlab-Token  header  string  false  "GitLab webhook secret token"
// @Param        payload         body    object  true   "GitLab push event payload"
// @Success      200 {object} WebhookResponse
// @Failure      400 {object} ErrorResponse
// @Failure      403 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /webhook [post]
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("read body: %v", err))
		return
	}
	defer r.Body.Close()

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

	proj, err := parser.ParseDir(s.cfg.PipelineDir)
	if err != nil {
		log.Printf("pipeline parse error: %v", err)
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("pipeline parse error: %v", err))
		return
	}

	changedRepos := s.classifyChanges(event, proj)

	engine := dag.New(proj)
	nodes, err := engine.Resolve(changedRepos)
	if err != nil {
		log.Printf("DAG resolve error: %v", err)
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("DAG error: %v", err))
		return
	}

	log.Printf("resolved %d tasks to execute", len(nodes))

	yamlContent := GenerateGitLabCI(proj, nodes)

	result := map[string]interface{}{
		"status":        "ok",
		"project":       proj.Name,
		"repo":          event.Repo,
		"branch":        event.Branch,
		"changed_repos": changedRepos,
		"tasks":         nodesToJSON(nodes),
		"gitlab_ci":     yamlContent,
	}

	if s.cfg.Dispatch != nil && len(nodes) > 0 {
		store := buildVarStore(proj)
		envVars := store.Get(event.Repo, "", event.Branch)

		dresp, err := s.cfg.Dispatch.Dispatch(dispatch.DispatchRequest{
			ProjectID: s.cfg.DispatchProjectID,
			Ref:       event.Branch,
			YAML:      yamlContent,
			Variables: envVars,
		})
		if err != nil {
			log.Printf("dispatch error: %v", err)
			result["dispatch_error"] = err.Error()
		} else {
			log.Printf("dispatched pipeline %d to project %d", dresp.PipelineID, s.cfg.DispatchProjectID)
			result["dispatch"] = map[string]interface{}{
				"pipeline_id": dresp.PipelineID,
				"iid":         dresp.IID,
				"status":      dresp.Status,
				"web_url":     dresp.WebURL,
			}
		}
	}

	s.writeJSON(w, http.StatusOK, result)
}

// --- Helpers ---

// WebhookResponse is the API response for the webhook endpoint.
type WebhookResponse struct {
	Status       string                   `json:"status"`
	Project      string                   `json:"project"`
	Repo         string                   `json:"repo"`
	Branch       string                   `json:"branch"`
	ChangedRepos map[string]bool          `json:"changed_repos"`
	Tasks        []map[string]interface{} `json:"tasks"`
	GitlabCI     string                   `json:"gitlab_ci"`
	Dispatch     *DispatchInfo            `json:"dispatch,omitempty"`
}

// DispatchInfo is included when a pipeline was dispatched to GitLab.
type DispatchInfo struct {
	PipelineID int    `json:"pipeline_id"`
	IID        int    `json:"iid"`
	Status     string `json:"status"`
	WebURL     string `json:"web_url"`
}

// ErrorResponse is the API error response.
type ErrorResponse struct {
	Error string `json:"error"`
}

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

func buildVarStore(proj model.Project) *variables.Store {
	store := variables.New()
	for _, v := range proj.Variables {
		opts := []variables.Option{}
		if v.Protected {
			opts = append(opts, variables.WithProtected(true))
		}
		if v.Masked {
			opts = append(opts, variables.WithMasked(true))
		}
		val := v.Value
		if val == "" {
			val = v.Default
		}
		switch v.Scope {
		case "repo":
			store.SetRepo(v.Repo, v.Key, val, opts...)
		case "job":
			store.SetJob(v.Repo, v.Job, v.Key, val, opts...)
		default:
			store.SetProject(v.Key, val, opts...)
		}
		if v.Env != "" {
			store.SetEnvFilter(v.Key, v.Env)
		}
	}
	return store
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
