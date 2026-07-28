package web

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gitlab.com/lunaris-bot/metaci/internal/domain"
	"gitlab.com/lunaris-bot/metaci/internal/parser"
	"gitlab.com/lunaris-bot/metaci/internal/service"
	"gitlab.com/lunaris-bot/metaci/internal/store"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"
	"gitlab.com/lunaris-bot/metaci/docs"
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
	cfg            Config
	router         chi.Router
	webhookLog     []map[string]interface{}
	webhookMu      sync.RWMutex
	projectStore   domain.ProjectStore
	runStore       domain.RunStore // nil when using FileStore (no run persistence)
	projectService *service.ProjectService
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
			s.projectStore = pg
			s.runStore = pg
		}
	}
	if s.projectStore == nil {
		storePath := cfg.StorePath
		if storePath == "" {
			storePath = store.DefaultPath()
		}
		fs, err := store.New(storePath)
		if err != nil {
			log.Printf("warning: could not init project store at %s: %v", storePath, err)
			fallback := filepath.Join(os.TempDir(), "metaci-fallback-projects.json")
			fs, _ = store.New(fallback)
		}
		s.projectStore = fs
	}

	s.projectService = service.NewProjectService(s.projectStore)

	// Auto-create project from --dir on startup.
	if cfg.PipelineDir != "" {
		if proj, err := parser.ParseDir(cfg.PipelineDir); err == nil && proj.Name != "" {
			_ = s.projectStore.Upsert(&domain.ProjectConfig{
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
		r.Get("/dashboard", s.handleDashboard)
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
