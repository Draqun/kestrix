package web

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kestrix/internal/credentials"
	"kestrix/internal/domain"
	"kestrix/internal/runner"
	"kestrix/internal/service"
	"kestrix/internal/source"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"
	"kestrix/docs"
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
	// StorePath is retained only for source compatibility with old callers;
	// PostgreSQL is the sole runtime persistence backend.
	StorePath     string
	DatabaseURL   string
	MasterKeyPath string
	Branch        string
	Docker        DockerConfig
	Signature     SignatureConfig
	// WebhookHistoryLimit bounds retained webhook history. Zero selects the
	// store default.
	WebhookHistoryLimit int
	Dispatch            DispatchConfig
	Runner              RunnerConfig
	SourceCache         SourceCacheConfig
}

// SourceCacheConfig controls the private persistent Git object cache.
type SourceCacheConfig struct {
	Directory string
	MaxAge    time.Duration
	MaxBytes  int64
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
	cfg             Config
	router          chi.Router
	webhookLogStore domain.WebhookLogStore
	projectStore    domain.ProjectStore
	runStore        domain.RunStore
	pipelineRunner  domain.PipelineRunner
	taskRunner      domain.TaskRunner
	projectService  *service.ProjectService
	runService      *service.RunService
	webhookService  *service.WebhookService
	credentialStore domain.CredentialStore
	sourceSessions  domain.SourceSessionManager
}

var listenAndServe = http.ListenAndServe

var newSourceSessionManager = func(opts source.CacheOptions) (domain.SourceSessionManager, error) {
	return source.NewGitCache(opts)
}

func (s *Server) credentialResolver() domain.CredentialResolver {
	if s.credentialStore == nil {
		return nil
	}
	return &source.CredentialStoreResolver{Store: s.credentialStore}
}

// NewServerE creates a server and returns persistence initialization errors
// before any HTTP listener is started.
func NewServerE(cfg Config) (*Server, error) {
	projectStore, runStore, err := initStores(cfg)
	if err != nil {
		return nil, err
	}
	projectService := service.NewProjectService(projectStore)
	pipelineRunner := &runner.PipelineExecutor{Mode: cfg.Runner.Mode, GenerateYAML: GenerateGitLabCI}
	taskRunner := pipelineRunner
	runService := service.NewRunService(runStore, pipelineRunner, taskRunner)
	webhookSink, _ := projectStore.(domain.WebhookLogSink)
	webhookService := service.NewWebhookService(webhookSink)
	s := NewServerWithDeps(cfg, projectStore, runStore, projectService, pipelineRunner, taskRunner, runService, webhookService)
	if logStore, ok := projectStore.(domain.WebhookLogStore); ok {
		s.webhookLogStore = logStore
	}
	if credentials, ok := projectStore.(domain.CredentialStore); ok {
		s.credentialStore = credentials
	}
	cacheCfg := cfg.SourceCache
	if cacheCfg.Directory == "" {
		cacheCfg.Directory = filepath.Join(os.TempDir(), "kestrix-source-cache")
	}
	if cacheCfg.MaxAge <= 0 {
		cacheCfg.MaxAge = 30 * 24 * time.Hour
	}
	if cacheCfg.MaxBytes <= 0 {
		cacheCfg.MaxBytes = 10 << 30
	}
	cache, err := newSourceSessionManager(source.CacheOptions{
		Root: cacheCfg.Directory, MaxAge: cacheCfg.MaxAge, MaxBytes: cacheCfg.MaxBytes,
		Resolver: s.credentialResolver(),
	})
	if err != nil {
		return nil, fmt.Errorf("initialize source cache: %w", err)
	}
	s.sourceSessions = cache
	maintenanceCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cache.Maintain(maintenanceCtx); err != nil && maintenanceCtx.Err() == nil {
		log.Printf("source cache maintenance warning: %v", err)
	}
	return s, nil
}

// NewServerWithDeps creates a server from prebuilt dependencies.
func NewServerWithDeps(
	cfg Config,
	projectStore domain.ProjectStore,
	runStore domain.RunStore,
	projectService *service.ProjectService,
	pipelineRunner domain.PipelineRunner,
	taskRunner domain.TaskRunner,
	runService *service.RunService,
	webhookService *service.WebhookService,
) *Server {
	s := &Server{
		cfg:            cfg,
		projectStore:   projectStore,
		runStore:       runStore,
		pipelineRunner: pipelineRunner,
		taskRunner:     taskRunner,
		projectService: projectService,
		runService:     runService,
		webhookService: webhookService,
	}
	if s.webhookService == nil {
		s.webhookService = service.NewWebhookService(nil)
	}
	s.router = s.buildRouter()
	return s
}

func initStores(cfg Config) (domain.ProjectStore, domain.RunStore, error) {
	if cfg.DatabaseURL == "" {
		return nil, nil, fmt.Errorf("database URL is required")
	}
	if cfg.MasterKeyPath == "" {
		return nil, nil, fmt.Errorf("credential master key unavailable: KESTRIX_MASTER_KEY_PATH is required")
	}
	provider := credentials.FileMasterKeyProvider{Path: cfg.MasterKeyPath, Version: 1}
	if _, _, err := provider.Current(); err != nil {
		return nil, nil, fmt.Errorf("validate KESTRIX_MASTER_KEY_PATH: %w", err)
	}
	projectStore, runStore, err := newPGStores(cfg.DatabaseURL)
	if err != nil {
		message := strings.ReplaceAll(err.Error(), cfg.DatabaseURL, "[redacted database URL]")
		return nil, nil, fmt.Errorf("initialize PostgreSQL: %s", message)
	}
	if setter, ok := projectStore.(interface {
		SetCredentialMasterKeyProvider(credentials.MasterKeyProvider)
	}); ok {
		setter.SetCredentialMasterKeyProvider(provider)
	}
	if setter, ok := projectStore.(interface{ SetWebhookHistoryLimit(int) }); ok {
		setter.SetWebhookHistoryLimit(cfg.WebhookHistoryLimit)
	}
	log.Printf("connected to PostgreSQL")
	return projectStore, runStore, nil
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
		r.Get("/credentials", s.handleListCredentials)
		r.Post("/credentials", s.handleCreateCredential)
		r.Get("/credentials/{ref}", s.handleGetCredential)
		r.Put("/credentials/{ref}", s.handleRotateCredential)
		r.Post("/credentials/{ref}/revoke", s.handleRevokeCredential)
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
	return listenAndServe(s.cfg.ListenAddr, s.router)
}
