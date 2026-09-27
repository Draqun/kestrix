package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kestrix/internal/auth"
	"kestrix/internal/credentials"
	"kestrix/internal/domain"
	"kestrix/internal/metrics"
	"kestrix/internal/redact"
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
	StorePath         string
	DatabaseURL       string
	MasterKeyPath     string
	JWTSigningKeyPath string
	// IdentityCacheTTL bounds how long RequireAuth may serve an account's
	// role and token version from cache; see design.md D4. Zero disables
	// the cache. config.Environment.IdentityCacheTTL already resolves the
	// operator-facing 5s default before this field is populated in
	// production wiring; here, zero simply means disabled.
	IdentityCacheTTL time.Duration
	// LoginMaxAttempts and LoginWindow configure the login throttle; see
	// design.md D7. Either being <= 0 disables throttling entirely -
	// config.Environment resolves the operator-facing 10/1m defaults
	// before populating these in production wiring.
	LoginMaxAttempts int
	LoginWindow      time.Duration
	// TrustProxyHeader makes the login throttle key on the first hop of
	// X-Forwarded-For instead of the raw peer address; see design.md D7.
	TrustProxyHeader bool
	// ContentSecurityPolicy overrides defaultContentSecurityPolicy on every
	// route except /docs/*; see design.md D8. Empty keeps the default.
	ContentSecurityPolicy string
	// CookieSecure marks the session cookie Secure; see session-cookie-auth
	// design.md D4. config.Environment resolves the operator-facing true
	// default before populating this in production wiring.
	CookieSecure bool
	Branch       string
	Signature    SignatureConfig
	// WebhookHistoryLimit bounds retained webhook history. Zero selects the
	// store default.
	WebhookHistoryLimit int
	// RunHistoryLimit bounds retained terminal run history; see
	// run-concurrency-and-retention design.md D3. Zero selects the store
	// default.
	RunHistoryLimit int
	Dispatch        DispatchConfig
	Runner          RunnerConfig
	SourceCache     SourceCacheConfig
	// HTTPReadTimeout, HTTPWriteTimeout, and HTTPIdleTimeout configure the
	// http.Server built by ListenAndServe; see design.md D5. Zero in any
	// field means the operator-facing default (config.Environment resolves
	// 30s/120s/120s before populating these in production wiring).
	HTTPReadTimeout  time.Duration
	HTTPWriteTimeout time.Duration
	HTTPIdleTimeout  time.Duration
	// MaxBodyBytes bounds request bodies on the /api subtree.
	// WebhookMaxBodyBytes bounds POST /webhook bodies independently; see
	// design.md D5. Zero means the operator-facing default (1 MiB / 8 MiB).
	MaxBodyBytes        int64
	WebhookMaxBodyBytes int64
	// RunnerJobMaxBodyBytes bounds a runner-job request body; zero means the
	// operator-facing default (20 MiB). RunnerJobMaxConcurrentPolls caps how
	// many concurrent GET /api/runner-jobs/next long-polls one runner token
	// may hold open; zero means the default (2). See
	// remote-runner-registration design.md D4.
	RunnerJobMaxBodyBytes       int64
	RunnerJobMaxConcurrentPolls int
	Metrics                     MetricsConfig
}

// MetricsConfig controls the GET /metrics endpoint; see observability
// spec.md. Enabled defaults to false. Token is a secret and is never
// returned by any handler.
type MetricsConfig struct {
	Enabled bool
	Token   string
}

// SourceCacheConfig controls the private persistent Git object cache.
type SourceCacheConfig struct {
	Directory string
	MaxAge    time.Duration
	MaxBytes  int64
}

// RunnerConfig holds runner execution settings.
type RunnerConfig struct {
	Mode string // instance default: "local", "docker", or "remote"
	// MaxConcurrentRuns bounds how many runs execute at once; a run accepted
	// beyond it is queued. MaxOutputBytes bounds stored run output. See
	// run-concurrency-and-retention design.md D1/D4. Zero in either selects
	// the RunService default (unlimited concurrency, unbounded output).
	MaxConcurrentRuns int
	MaxOutputBytes    int
	// RemoteJobTimeout and RemoteJobMaxReclaims bound silent-runner
	// recovery (tasks.md 4.1/4.2). Zero in either selects the RunService
	// default (120s, 3 reclaims).
	RemoteJobTimeout     time.Duration
	RemoteJobMaxReclaims int
	// OfflineAfter is how long a runner holding no job may be silent before
	// GET /api/runners reports it offline. Zero selects the 60s default.
	OfflineAfter time.Duration
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
	userStore       domain.UserStore
	grantStore      domain.GrantStore
	tokenIssuer     domain.TokenIssuer
	passwordHasher  domain.PasswordHasher
	authService     *service.AuthService
	identityCache   domain.IdentityCache
	loginThrottle   *loginThrottle
	// setupThrottle guards POST /api/setup with its own address-keyed
	// window, configured from the same limits as loginThrottle but never
	// sharing its counters: setup answers non-201 forever once any account
	// exists, and counting that permanent response against loginThrottle's
	// shared per-address budget would let an unauthenticated caller trip
	// 429 on POST /api/auth/login for any other client behind the same
	// address. See first-run-setup design.md D4.
	setupThrottle *loginThrottle
	// metrics is the Recorder every FinishRun/CancelRun and webhook-outcome
	// call site in this package feeds. It is metrics.NoOp{} when
	// cfg.Metrics.Enabled is false, so nothing is collected or retained in
	// memory - see observability spec.md "Metrics disabled by default" and
	// design.md D2.
	metrics metrics.Recorder
	// runnerStore backs runner registration/authentication and the
	// runner-job route group; nil unless the configured RunStore also
	// implements domain.RunnerStore (PGStore does). See
	// remote-runner-registration.
	runnerStore domain.RunnerStore
	// remoteJobStore backs the runner-job endpoints' claim/trace/complete
	// calls; nil unless the configured RunStore also implements
	// domain.RemoteJobStore (PGStore does).
	remoteJobStore domain.RemoteJobStore
	// runnerPollLimiter bounds concurrent GET /api/runner-jobs/next
	// long-polls per authenticated runner token (design.md D4).
	runnerPollLimiter *runnerPollLimiter
}

// knownSecrets returns the control-plane secret values that could plausibly
// leak into an error message surfaced by this server's own operations
// (webhook signature verification, GitLab dispatch, metrics scrape auth), so
// a log call site can redact an err against them before writing it - see
// observability spec.md "Secrets never reach the log". A call site with no
// error in scope, or one that cannot plausibly surface one of these values,
// still redacts through the same helper for a single consistent pattern;
// see design.md's "Redaction misses a field" risk.
func (s *Server) knownSecrets() []string {
	return []string{s.cfg.Signature.Secret, s.cfg.Dispatch.GitLabToken, s.cfg.Metrics.Token}
}

// identityCacheMaxEntries bounds InMemoryIdentityCache regardless of how
// many distinct accounts have authenticated recently; see design.md D4.
const identityCacheMaxEntries = 10000

// loginThrottleMaxEntries bounds each of loginThrottle's account and
// address maps independently, regardless of how many distinct names or
// addresses POST /api/auth/login - public and unauthenticated - has seen.
const loginThrottleMaxEntries = 10000

// defaultHTTPReadTimeout, defaultHTTPWriteTimeout, and defaultHTTPIdleTimeout
// apply when Config leaves the corresponding field zero; see design.md D5.
// config.Environment already resolves these operator-facing defaults before
// populating Config in production wiring, so this fallback mainly serves
// tests and other direct Config construction.
const (
	defaultHTTPReadTimeout             = 30 * time.Second
	defaultHTTPWriteTimeout            = 120 * time.Second
	defaultHTTPIdleTimeout             = 120 * time.Second
	defaultMaxBodyBytes                = 1 << 20  // 1 MiB
	defaultWebhookMaxBytes             = 8 << 20  // 8 MiB
	defaultRunnerJobMaxBytes           = 20 << 20 // 20 MiB
	defaultRunnerJobMaxConcurrentPolls = 2
	defaultRemoteJobTimeout            = 120 * time.Second
	defaultRemoteJobMaxReclaims        = 3
)

// serveHTTP is a seam over (*http.Server).ListenAndServe so tests can inject
// a listen failure without opening a real socket; see design.md D5.
var serveHTTP = (*http.Server).ListenAndServe

// httpServer builds the *http.Server ListenAndServe runs, with timeouts
// resolved from cfg (falling back to the package defaults when a field is
// zero).
func (s *Server) httpServer() *http.Server {
	readTimeout := s.cfg.HTTPReadTimeout
	if readTimeout <= 0 {
		readTimeout = defaultHTTPReadTimeout
	}
	writeTimeout := s.cfg.HTTPWriteTimeout
	if writeTimeout <= 0 {
		writeTimeout = defaultHTTPWriteTimeout
	}
	idleTimeout := s.cfg.HTTPIdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = defaultHTTPIdleTimeout
	}
	return &http.Server{
		Addr:         s.cfg.ListenAddr,
		Handler:      s.router,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}
}

// maxBodyBytes and webhookMaxBodyBytes resolve cfg's body bounds, falling
// back to the package defaults when unset.
func (s *Server) maxBodyBytes() int64 {
	if s.cfg.MaxBodyBytes > 0 {
		return s.cfg.MaxBodyBytes
	}
	return defaultMaxBodyBytes
}

func (s *Server) webhookMaxBodyBytes() int64 {
	if s.cfg.WebhookMaxBodyBytes > 0 {
		return s.cfg.WebhookMaxBodyBytes
	}
	return defaultWebhookMaxBytes
}

func (s *Server) runnerJobMaxBodyBytes() int64 {
	if s.cfg.RunnerJobMaxBodyBytes > 0 {
		return s.cfg.RunnerJobMaxBodyBytes
	}
	return defaultRunnerJobMaxBytes
}

func (s *Server) runnerJobMaxConcurrentPolls() int {
	if s.cfg.RunnerJobMaxConcurrentPolls > 0 {
		return s.cfg.RunnerJobMaxConcurrentPolls
	}
	return defaultRunnerJobMaxConcurrentPolls
}

// maxBody returns middleware that bounds the request body to n bytes via
// http.MaxBytesReader; a body exceeding it fails downstream reads with
// *http.MaxBytesError, which decodeJSON and the webhook/login handlers map
// to 413. See design.md D5.
func maxBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

var newSourceSessionManager = func(opts source.CacheOptions) (domain.SourceSessionManager, error) {
	return source.NewGitCache(opts)
}

func (s *Server) credentialResolver() domain.CredentialResolver {
	if s.credentialStore == nil {
		return nil
	}
	return &source.CredentialStoreResolver{Store: s.credentialStore}
}

// secretResolver returns the domain.CredentialResolver used to resolve a
// pipeline variable's secret_ref (see runtime-secrets design.md D3). Unlike
// credentialResolver, it does not go through source.CredentialStoreResolver:
// that adapter refuses a `secret` credential (D4, for Git source
// acquisition), which is exactly the type BuildVarStore needs to resolve.
// BuildVarStore performs its own type check the other way - requiring
// Type == "secret" - so no Git credential can be injected as a variable.
func (s *Server) secretResolver() domain.CredentialResolver {
	if s.credentialStore == nil {
		return nil
	}
	return secretCredentialResolver{store: s.credentialStore}
}

// secretCredentialResolver adapts domain.CredentialStore directly to
// domain.CredentialResolver, with no credential-type gate of its own.
type secretCredentialResolver struct {
	store domain.CredentialStore
}

func (r secretCredentialResolver) Resolve(ctx context.Context, ref string) (domain.Credential, error) {
	return r.store.ResolveCredential(ctx, ref)
}

// NewServerE creates a server and returns persistence initialization errors
// before any HTTP listener is started.
func NewServerE(cfg Config) (*Server, error) {
	tokenIssuer, err := newTokenIssuer(cfg)
	if err != nil {
		return nil, err
	}
	projectStore, runStore, err := initStores(cfg)
	if err != nil {
		return nil, err
	}
	projectService := service.NewProjectService(projectStore)
	executor := modeSelectingExecutor{defaultMode: cfg.Runner.Mode}
	pipelineRunner := domain.PipelineRunner(executor)
	taskRunner := domain.TaskRunner(executor)
	runService := service.NewRunService(runStore, pipelineRunner, taskRunner)
	runService.SetMaxConcurrentRuns(cfg.Runner.MaxConcurrentRuns)
	runService.MaxOutputBytes = cfg.Runner.MaxOutputBytes
	if remoteJobs, ok := runStore.(domain.RemoteJobStore); ok {
		runService.SetRemoteJobStore(remoteJobs)
	}
	webhookSink, _ := projectStore.(domain.WebhookLogSink)
	webhookService := service.NewWebhookService(webhookSink)
	s := NewServerWithDeps(cfg, projectStore, runStore, projectService, pipelineRunner, taskRunner, runService, webhookService)
	if logStore, ok := projectStore.(domain.WebhookLogStore); ok {
		s.webhookLogStore = logStore
	}
	if credentials, ok := projectStore.(domain.CredentialStore); ok {
		s.credentialStore = credentials
		s.projectService.SetCredentialLookup(credentials.GetCredentialMetadata)
	}
	if users, ok := projectStore.(domain.UserStore); ok {
		s.userStore = users
	}
	if grants, ok := projectStore.(domain.GrantStore); ok {
		s.grantStore = grants
	}
	s.tokenIssuer = tokenIssuer
	s.passwordHasher = auth.NewArgon2Hasher()
	if s.userStore != nil {
		s.authService = service.NewAuthService(s.userStore, s.passwordHasher, s.tokenIssuer)
	}
	s.identityCache = auth.NewInMemoryIdentityCache(cfg.IdentityCacheTTL, identityCacheMaxEntries, time.Now)
	if cfg.LoginMaxAttempts > 0 && cfg.LoginWindow > 0 {
		s.loginThrottle = newLoginThrottle(cfg.LoginMaxAttempts, cfg.LoginWindow, loginThrottleMaxEntries, time.Now)
		s.setupThrottle = newLoginThrottle(cfg.LoginMaxAttempts, cfg.LoginWindow, loginThrottleMaxEntries, time.Now)
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
		slog.Warn("source cache maintenance warning", "err", redact.Err(err, s.knownSecrets()))
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
	if runners, ok := runStore.(domain.RunnerStore); ok {
		s.runnerStore = runners
	}
	if remoteJobs, ok := runStore.(domain.RemoteJobStore); ok {
		s.remoteJobStore = remoteJobs
	}
	s.runnerPollLimiter = newRunnerPollLimiter()
	if cfg.Metrics.Enabled {
		s.metrics = metrics.NewRegistry()
	} else {
		s.metrics = metrics.NoOp{}
	}
	if s.runService != nil {
		s.runService.Metrics = s.metrics
	}
	s.router = s.buildRouter()
	return s
}

// newTokenIssuer validates the configured JWT signing key and returns a
// domain.TokenIssuer backed by it. It fails before any database work starts,
// mirroring how the credential master key is validated.
func newTokenIssuer(cfg Config) (domain.TokenIssuer, error) {
	if cfg.JWTSigningKeyPath == "" {
		return nil, fmt.Errorf("token signing key unavailable: KESTRIX_JWT_SIGNING_KEY_PATH is required")
	}
	provider := auth.FileSigningKeyProvider{Path: cfg.JWTSigningKeyPath}
	if _, err := provider.Key(); err != nil {
		return nil, fmt.Errorf("validate KESTRIX_JWT_SIGNING_KEY_PATH: %w", err)
	}
	return auth.JWTIssuer{Key: provider.Key}, nil
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
	projectStore, runStore, err := newPGStores(cfg.DatabaseURL, cfg.Runner.Mode)
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
	if setter, ok := runStore.(interface{ SetRunHistoryLimit(int) }); ok {
		setter.SetRunHistoryLimit(cfg.RunHistoryLimit)
	}
	slog.Info("connected to PostgreSQL")
	return projectStore, runStore, nil
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()

	r.Use(s.SecurityHeaders)
	r.Use(chiMiddleware.RequestID)
	r.Use(slogRequestLogger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Heartbeat("/ping"))

	// API routes. Every route is either on the public group (no token
	// required) or on the protected group (RequireAuth, then a per-group
	// RequireRole minimum) - see access-control spec "Authentication
	// required on API routes": a route added to neither is unreachable,
	// never accidentally open.
	r.Route("/api", func(r chi.Router) {
		r.Use(maxBody(s.maxBodyBytes()))
		r.Group(func(r chi.Router) {
			r.Get("/health", s.handleHealth)
			r.With(s.throttleLogin).Post("/auth/login", s.handleLogin)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/setup", s.handleSetupStatus)
			r.With(s.throttleSetup).Post("/setup", s.handleCompleteSetup)
		})

		r.Group(func(r chi.Router) {
			r.Use(s.RequireAuth)

			r.Group(func(r chi.Router) {
				r.Use(s.RequireRole(domain.RoleViewer))
				r.Get("/auth/me", s.handleMe)
				r.Get("/dashboard", s.handleDashboard)
				r.Get("/pipeline", s.handleGetPipeline)
				r.Get("/pipeline/raw", s.handleGetPipelineRaw)
				r.Get("/projects", s.handleListProjects)
				r.Get("/projects/{name}", s.handleGetProject)
				r.Get("/projects/{name}/definition", s.handleGetProjectDefinition)
				r.Get("/projects/{name}/pipeline", s.handleGetProjectPipeline)
				r.Post("/projects/{name}/resolve", s.handleResolveProject)
				r.Get("/projects/{name}/runs", s.handleListRuns)
				r.Get("/projects/{name}/runs/{runID}", s.handleGetRun)
				r.Get("/runs", s.handleListAllRuns)
				r.Post("/resolve", s.handleResolve)
				r.Post("/generate", s.handleGenerate)
				r.Get("/validate", s.handleValidate)
				r.Get("/logs", s.handleGetLogs)
				r.Get("/version", s.handleVersion)
			})

			r.Group(func(r chi.Router) {
				r.Use(s.RequireRole(domain.RoleOperator))
				r.Post("/projects/{name}/pipeline", s.handleSaveProjectPipeline)
				r.Post("/projects/{name}/run", s.handleRunProject)
				r.Post("/projects/{name}/tasks/{task}/run", s.handleRunTask)
				r.Post("/projects/{name}/runs/{runID}/cancel", s.handleCancelRun)
				r.Post("/projects/{name}/runs/{runID}/tasks/{task}/run", s.handleApproveManualTask)
			})

			r.Group(func(r chi.Router) {
				r.Use(s.RequireRole(domain.RoleAdmin))
				r.Get("/credentials", s.handleListCredentials)
				r.Post("/credentials", s.handleCreateCredential)
				r.Get("/credentials/{ref}", s.handleGetCredential)
				r.Put("/credentials/{ref}", s.handleRotateCredential)
				r.Post("/credentials/{ref}/revoke", s.handleRevokeCredential)
				r.Post("/projects", s.handleCreateProject)
				r.Put("/projects/{name}", s.handleUpdateProject)
				r.Delete("/projects/{name}", s.handleDeleteProject)
				r.Get("/users", s.handleListUsers)
				r.Post("/users", s.handleCreateUser)
				r.Put("/users/{user}/role", s.handleUpdateUserRole)
				r.Put("/users/{user}/grants", s.handleSetUserGrants)
				r.Delete("/users/{user}", s.handleDeleteUser)
				r.Get("/runners", s.handleListRunners)
				r.Post("/runners", s.handleCreateRunner)
				r.Delete("/runners/{id}", s.handleRevokeRunner)
			})
		})
	})

	// Webhook
	r.With(maxBody(s.webhookMaxBodyBytes())).Post("/webhook", s.handleWebhook)

	// Runner-job endpoints - outside /api, their own bearer-token auth
	// (requireRunnerAuth) and body-size bound, the same shape /webhook uses
	// for its own independent auth; see remote-runner-registration
	// design.md D4 and tasks.md 3.1.
	r.Route("/api/runner-jobs", func(r chi.Router) {
		r.Use(maxBody(s.runnerJobMaxBodyBytes()))
		r.Use(s.requireRunnerAuth)
		r.Get("/next", s.handleRunnerJobNext)
		r.Patch("/{id}/trace", s.handleRunnerJobTrace)
		r.Put("/{id}/complete", s.handleRunnerJobComplete)
	})

	// Metrics - registered only when enabled, so a disabled instance
	// answers /metrics with the router's ordinary 404 and no handler exists
	// to leak timing or headers; see observability design.md D6 and spec.md
	// "Metrics disabled by default".
	if s.cfg.Metrics.Enabled {
		r.With(s.requireMetricsAuth).Get("/metrics", s.handleMetrics)
	}

	// Swagger docs - its own, more permissive content security policy;
	// see design.md D8.
	r.Group(func(r chi.Router) {
		r.Use(docsSecurityHeaders)
		r.Get("/docs/swagger.json", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(docs.SwaggerInfo.ReadDoc()))
		})
		r.Get("/docs/*", httpSwagger.Handler(
			httpSwagger.URL("/docs/swagger.json"),
		))
	})

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

// Router returns the server's HTTP handler, for embedding or for driving it
// in tests (httptest.NewRecorder + Router().ServeHTTP) without opening a
// real listener.
func (s *Server) Router() http.Handler {
	return s.router
}

// FailInterruptedRuns reconciles runs left running by a previous instance.
// Callers (cmdServe) run this after NewServerE and before ListenAndServe, so
// the HTTP listener never serves a request that can observe a ghost run.
func (s *Server) FailInterruptedRuns(reason string) (int, error) {
	return s.runStore.FailInterruptedRuns(reason)
}

// remoteJobTimeout and remoteJobMaxReclaims resolve cfg's silent-runner
// recovery bounds, falling back to the package defaults when unset - see
// remote-runner-registration tasks.md 4.1.
func (s *Server) remoteJobTimeout() time.Duration {
	if s.cfg.Runner.RemoteJobTimeout > 0 {
		return s.cfg.Runner.RemoteJobTimeout
	}
	return defaultRemoteJobTimeout
}

// defaultRunnerOfflineAfter mirrors the KESTRIX_RUNNER_OFFLINE_AFTER default
// (internal/config), for a Config built without it. 60s was chosen as three
// of runnerLongPollInterval's 20s periods, so one slow poll never flaps a
// runner offline; revisit it if that interval changes. It is a literal, not
// derived, because runnerLongPollInterval is a var tests shorten.
const defaultRunnerOfflineAfter = 60 * time.Second

func (s *Server) runnerOfflineAfter() time.Duration {
	if s.cfg.Runner.OfflineAfter > 0 {
		return s.cfg.Runner.OfflineAfter
	}
	return defaultRunnerOfflineAfter
}

func (s *Server) remoteJobMaxReclaims() int {
	if s.cfg.Runner.RemoteJobMaxReclaims > 0 {
		return s.cfg.Runner.RemoteJobMaxReclaims
	}
	return defaultRemoteJobMaxReclaims
}

// remoteJobReclaimInterval is how often StartRemoteJobReclaimLoop checks for
// stale jobs - independent of, and shorter than, the timeout that decides
// whether a job counts as stale, so a job crosses its timeout within one
// interval of it actually happening. A package variable, not a constant, so
// a unit test isn't forced to wait the real interval.
var remoteJobReclaimInterval = 15 * time.Second

// StartRemoteJobReclaimLoop runs silent-runner recovery (tasks.md 4.2) on a
// fixed interval until ctx is done. It is a no-op loop (ticks but does
// nothing) when no remoteJobStore is configured, so a caller can start it
// unconditionally at server startup without checking first - the same
// pattern reconcileInterruptedRuns's caller uses for FailInterruptedRuns.
func (s *Server) StartRemoteJobReclaimLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(remoteJobReclaimInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if s.runService == nil {
					continue
				}
				if err := s.runService.DeleteTerminalRemoteSecrets(); err != nil {
					slog.Warn("deleting finished remote jobs' secrets failed", "err", redact.Err(err, s.knownSecrets()))
				}
				reclaimed, failed, err := s.runService.ReclaimStaleRemoteJobs(
					s.remoteJobTimeout(), s.remoteJobMaxReclaims(), "runner went silent",
				)
				if err != nil {
					slog.Warn("silent-runner recovery check failed", "err", redact.Err(err, s.knownSecrets()))
					continue
				}
				if reclaimed > 0 || failed > 0 {
					slog.Info("silent-runner recovery", "reclaimed", reclaimed, "failed", failed)
				}
			}
		}
	}()
}

// ListenAndServe starts the server.
func (s *Server) ListenAndServe() error {
	slog.Info("Kestrix server listening", "addr", s.cfg.ListenAddr)
	return serveHTTP(s.httpServer())
}
