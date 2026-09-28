// Package main is the Kestrix CLI entry point.
//
// Kestrix — Repo-agnostic CI/CD Control Plane
//
// @title           Kestrix API
// @version         0.2.0
// @description     Repo-agnostic CI/CD orchestrator with HCL-based DAG pipeline definitions.
// @description     Supports OR/AND/NOT trigger logic for cross-repo conditional builds.
//
// @contact.name   Kestrix Team
// @contact.email  damian.giebas@gmail.com
//
// @host           localhost:8080
// @BasePath       /
//
// @securityDefinitions.apikey  WebhookToken
// @in                          header
// @name                        X-Gitlab-Token
// @description                 GitLab webhook secret token for signature verification
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"kestrix/internal/config"
	"kestrix/internal/dag"
	"kestrix/internal/parser"
	"kestrix/internal/runner"
	"kestrix/internal/variables"
	"kestrix/internal/version"
	"kestrix/internal/web"
)

const defaultLocalDatabaseURL = "postgres://kestrix:kestrix@localhost:5433/kestrix_local?sslmode=disable"

func main() {
	config.Initialize()
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "resolve":
		cmdResolve(os.Args[2:])
	case "validate":
		cmdValidate(os.Args[2:])
	case "run":
		cmdRun(os.Args[2:])
	case "serve":
		cmdServe(os.Args[2:])
	case "runner":
		cmdRunner(os.Args[2:])
	case "variables":
		cmdVariables(os.Args[2:])
	case "user":
		cmdUser(os.Args[2:])
	case "version":
		fmt.Println(version.Version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `kestrix — repo-agnostic CI/CD orchestrator

Usage:
  kestrix resolve    [--changed repo1,repo2] <file.tf>
  kestrix validate   <file.tf>
  kestrix run        [--changed repo1,repo2] [--workdir .] [--include-manual] <file.tf>
  kestrix serve      [--addr :8080] [--database-url postgres://...] [--static ../ui/dist] [--secret TOKEN] [--dispatch PROJECT_ID]
  kestrix runner     --server https://kestrix.example.com --token TOKEN [--mode local|docker]
  kestrix variables  <file.tf> [--repo auth_service] [--job deploy] [--env staging]
  kestrix user create [--role viewer|operator|admin] <name>
  kestrix user list
  kestrix user passwd <name>
  kestrix user delete <name>
  kestrix version`)
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	changedFlag := fs.String("changed", "", "comma-separated list of changed repo names")
	workDir := fs.String("workdir", ".", "checkout containing the pipeline repositories")
	includeManual := fs.Bool("include-manual", false, "run tasks marked manual")
	localFlag := fs.Bool("local", true, "run tasks locally (no Docker)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: pipeline file required")
		os.Exit(2)
	}
	proj, err := parser.ParseFile(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		os.Exit(1)
	}
	changed := make(map[string]bool, len(proj.Repos))
	for _, repo := range proj.Repos {
		changed[repo.Name] = false
	}
	for _, name := range strings.Split(*changedFlag, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			changed[name] = true
		}
	}
	opts := runner.Options{
		WorkDir:       *workDir,
		IncludeManual: *includeManual,
		Stdout:        os.Stdout,
		Stderr:        os.Stderr,
	}
	if *localFlag {
		_, err = runner.RunLocal(context.Background(), proj, changed, opts)
	} else {
		_, err = runner.Run(context.Background(), proj, changed, opts)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "pipeline failed: %v\n", err)
		os.Exit(1)
	}
}

func resolveWebhookMethod(flagMethod string, env *config.Environment) string {
	// Effective webhook verification method: an explicit --method flag
	// wins, otherwise KESTRIX_WEBHOOK_METHOD applies. Empty means the
	// verifier default (gitlab).
	if flagMethod != "" {
		return flagMethod
	}
	if env == nil {
		return ""
	}
	return env.WebhookMethod
}

// buildServeConfig converts the reconciled config plus the environment-only
// settings into web.Config. Every file-or-flag setting comes from cfg; only
// env-only settings (key paths, throttle, CSP, cookie, HTTP bounds) come
// from env. It is pure so the precedence table can test it without starting
// the server.
func buildServeConfig(cfg *config.Config, env *config.Environment, staticDir, method string) web.Config {
	dbURL := cfg.DatabaseURL
	if dbURL == "" {
		dbURL = defaultLocalDatabaseURL
	}
	return web.Config{
		ListenAddr:            cfg.ListenAddr,
		PipelineDir:           cfg.Pipeline.Dir,
		StaticDir:             staticDir,
		DatabaseURL:           dbURL,
		MasterKeyPath:         env.MasterKeyPath,
		JWTSigningKeyPath:     env.JWTSigningKeyPath,
		IdentityCacheTTL:      env.IdentityCacheTTL,
		LoginMaxAttempts:      env.LoginMaxAttempts,
		LoginWindow:           env.LoginWindow,
		TrustProxyHeader:      env.TrustProxyHeader,
		ContentSecurityPolicy: env.ContentSecurityPolicy,
		CookieSecure:          env.CookieSecure,
		Branch:                cfg.Branch,
		SourceCache: web.SourceCacheConfig{
			Directory: cfg.SourceCache.Directory,
			MaxAge:    time.Duration(cfg.SourceCache.MaxAge),
			MaxBytes:  cfg.SourceCache.MaxBytes,
		},
		HTTPReadTimeout:             env.HTTPReadTimeout,
		HTTPWriteTimeout:            env.HTTPWriteTimeout,
		HTTPIdleTimeout:             env.HTTPIdleTimeout,
		MaxBodyBytes:                env.HTTPMaxBodyBytes,
		WebhookMaxBodyBytes:         env.WebhookMaxBodyBytes,
		RunnerJobMaxBodyBytes:       env.RunnerJobMaxBodyBytes,
		RunnerJobMaxConcurrentPolls: env.RunnerJobMaxConcurrentPolls,
		Signature: web.SignatureConfig{
			Secret: cfg.Webhook.Secret,
			Method: method,
		},
		WebhookHistoryLimit: cfg.Webhook.HistoryLimit,
		RunHistoryLimit:     cfg.Runner.RunHistoryLimit,
		Dispatch: web.DispatchConfig{
			Enabled:     cfg.Dispatch.Enabled,
			ProjectID:   fmt.Sprintf("%d", cfg.Dispatch.ProjectID),
			GitLabURL:   cfg.Dispatch.GitLabURL,
			GitLabToken: cfg.Dispatch.GitLabToken,
		},
		Runner: web.RunnerConfig{
			Mode:                 cfg.Runner.Mode,
			MaxConcurrentRuns:    cfg.Runner.MaxConcurrentRuns,
			MaxOutputBytes:       cfg.Runner.MaxOutputBytes,
			RemoteJobTimeout:     env.RunnerJobTimeout,
			RemoteJobMaxReclaims: env.RunnerJobMaxReclaims,
			OfflineAfter:         env.RunnerOfflineAfter,
			AllowLocal:           cfg.Runner.AllowLocal,
		},
		Metrics: web.MetricsConfig{
			Enabled: cfg.Metrics.Enabled,
			Token:   cfg.Metrics.Token,
		},
	}
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", config.DefaultPath(), "config file path")
	addr := fs.String("addr", "", "listen address (overrides config)")
	dir := fs.String("dir", "", "pipeline directory (overrides config)")
	secret := fs.String("secret", "", "webhook verification secret (env: KESTRIX_WEBHOOK_SECRET)")
	method := fs.String("method", "", "verification method: gitlab or hmac")
	dispatchFlag := fs.String("dispatch", "", "GitLab project ID to dispatch pipelines to (env: KESTRIX_DISPATCH_PROJECT)")
	gitlabURL := fs.String("gitlab-url", "", "GitLab instance URL (overrides config)")
	gitlabToken := fs.String("gitlab-token", "", "GitLab API token (env: KESTRIX_GITLAB_TOKEN)")
	branchFlag := fs.String("branch", "", "default branch (overrides config)")
	staticDir := fs.String("static", "", "directory to serve as static files (e.g. ../ui/dist)")
	dbURL := fs.String("database-url", "", "PostgreSQL connection URL (env: DATABASE_URL)")
	sourceCacheDir := fs.String("source-cache-dir", "", "Git object cache directory (env: KESTRIX_SOURCE_CACHE_DIR)")
	sourceCacheMaxAge := fs.Duration("source-cache-max-age", 0, "maximum age of an unused Git cache entry (env: KESTRIX_SOURCE_CACHE_MAX_AGE)")
	sourceCacheMaxBytes := fs.Int64("source-cache-max-bytes", 0, "maximum total Git cache bytes (env: KESTRIX_SOURCE_CACHE_MAX_BYTES)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	env := config.Current()

	// Load config from file (defaults if missing), then reconcile once:
	// file → environment (absent keys only) → flags (non-zero wins).
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.ApplyEnvironment(env); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	env = env.WithRunnerConfig(cfg)
	config.ReplaceCurrent(env)
	cacheAgeSet, cacheBytesSet := false, false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "source-cache-max-age":
			cacheAgeSet = true
		case "source-cache-max-bytes":
			cacheBytesSet = true
		}
	})
	if cacheAgeSet && *sourceCacheMaxAge <= 0 {
		fmt.Fprintln(os.Stderr, "config: source-cache-max-age must be positive")
		os.Exit(1)
	}
	if cacheBytesSet && *sourceCacheMaxBytes <= 0 {
		fmt.Fprintln(os.Stderr, "config: source-cache-max-bytes must be positive")
		os.Exit(1)
	}
	if err := cfg.ApplyFlags(config.FlagValues{
		ListenAddr:          *addr,
		PipelineDir:         *dir,
		Branch:              *branchFlag,
		WebhookSecret:       *secret,
		DispatchProjectRaw:  *dispatchFlag,
		GitLabURL:           *gitlabURL,
		GitLabToken:         *gitlabToken,
		DatabaseURL:         *dbURL,
		SourceCacheDir:      *sourceCacheDir,
		SourceCacheMaxAge:   *sourceCacheMaxAge,
		SourceCacheMaxBytes: *sourceCacheMaxBytes,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.SourceCache.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.Runner.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	if err := installLogging(os.Stderr, cfg.Logging.Level); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	// Webhook method has no yaml form: flag wins, otherwise env.
	*method = resolveWebhookMethod(*method, env)

	webCfg := buildServeConfig(cfg, env, *staticDir, *method)
	logConfigSources(os.Stderr, cfg, *configPath)

	server, err := web.NewServerE(webCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "server startup error: %v\n", err)
		os.Exit(1)
	}
	if err := reconcileInterruptedRuns(server, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "server startup error: %v\n", err)
		os.Exit(1)
	}
	if err := warnLocalModeProjects(server, cfg.Runner.AllowLocal, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "server startup error: %v\n", err)
		os.Exit(1)
	}
	server.StartRemoteJobReclaimLoop(context.Background())
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

// runReconciler is satisfied by *web.Server in production and by a fake
// store in tests, so reconcileInterruptedRuns needs neither a real server
// nor internal/domain.
type runReconciler interface {
	FailInterruptedRuns(reason string) (int, error)
}

// localModeProjectCounter is satisfied by *web.Server in production and by
// a fake in tests, mirroring runReconciler above.
type localModeProjectCounter interface {
	CountLocalModeProjects() (int, error)
}

// warnLocalModeProjects logs the runner.allow_local exposure warning,
// naming how many stored projects resolve to `local` execution - see
// safe-production-execution spec.md "Opt-in accepted for a single-user
// machine". A no-op when allowLocal is false: in that state no local
// execution can happen at all, so there is nothing to warn about.
func warnLocalModeProjects(c localModeProjectCounter, allowLocal bool, w io.Writer) error {
	if !allowLocal {
		return nil
	}
	n, err := c.CountLocalModeProjects()
	if err != nil {
		return fmt.Errorf("count local-mode projects: %w", err)
	}
	fmt.Fprintf(w, "runner: KESTRIX_RUNNER_ALLOW_LOCAL is true; %d stored project(s) execute in local mode and can read this instance's key files\n", n)
	return nil
}

// reconcileInterruptedRuns fails every run left "running" by a previous
// kestrix instance before the HTTP listener opens, so a restart cannot
// leave ghost runs in lists, counters, and guards. It logs the count,
// including zero, so every start reports whether it found any.
func reconcileInterruptedRuns(r runReconciler, w io.Writer) error {
	n, err := r.FailInterruptedRuns("interrupted by server restart")
	if err != nil {
		return fmt.Errorf("reconcile interrupted runs: %w", err)
	}
	fmt.Fprintf(w, "runs: failed %d interrupted run(s) from a previous instance\n", n)
	return nil
}

// parseLogLevel maps a validated logging.level string to its slog.Level.
// cfg.ApplyEnvironment already rejected any value outside the four accepted
// ones before this runs, so the default case is unreachable in production -
// it exists so a caller passing an unvalidated string fails loudly instead
// of silently defaulting to Info.
func parseLogLevel(level string) (slog.Level, error) {
	switch level {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("logging.level must be one of debug, info, warn, error, got %q", level)
	}
}

// installLogging installs a JSON slog handler at level as the process-wide
// default, writing to w. slog.SetDefault also redirects the stdlib log
// package's default output through the same handler (see the log/slog
// package doc for SetDefault), so every log.Printf call site - including
// chi middleware.Logger's own request lines - shares the same JSON stream
// without a separate log.SetOutput call; see observability design.md D7 and
// spec.md "Structured process logging". Call it once, before the server and
// runner are constructed.
func installLogging(w io.Writer, level string) error {
	lvl, err := parseLogLevel(level)
	if err != nil {
		return err
	}
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl})
	slog.SetDefault(slog.New(handler))
	slog.SetLogLoggerLevel(lvl)
	return nil
}

// logConfigSources names which source won for each multi-form setting
// without printing any value, so operators can audit precedence flips.
func logConfigSources(w io.Writer, cfg *config.Config, configPath string) {
	fmt.Fprintf(w, "config: %s\n", configPath)
	if _, statErr := os.Stat(configPath); os.IsNotExist(statErr) {
		fmt.Fprintf(w, "config: no config file found, using defaults + CLI flags\n")
	}
	for _, line := range cfg.SourceReport() {
		fmt.Fprintf(w, "config: %s\n", line)
	}
}

func cmdResolve(args []string) {
	fs := flag.NewFlagSet("resolve", flag.ExitOnError)
	changedFlag := fs.String("changed", "", "comma-separated list of changed repo names")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: pipeline file required")
		os.Exit(2)
	}
	path := fs.Arg(0)

	proj, err := parser.ParseFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		os.Exit(1)
	}

	changed := make(map[string]bool)
	for _, r := range proj.Repos {
		changed[r.Name] = false
	}
	if *changedFlag != "" {
		for _, name := range strings.Split(*changedFlag, ",") {
			name = strings.TrimSpace(name)
			if _, ok := changed[name]; !ok {
				fmt.Fprintf(os.Stderr, "warning: changed repo %q is not declared in pipeline\n", name)
			}
			changed[name] = true
		}
	}

	engine := dag.New(proj)
	nodes, err := engine.Resolve(changed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Project: %s\n", proj.Name)
	fmt.Printf("Changed repos: %s\n", changedSummary(changed))
	fmt.Printf("Tasks triggered (%d):\n", len(nodes))
	for _, n := range nodes {
		tag := "AUTO"
		if n.Task.Manual {
			tag = "MANUAL"
		}
		fmt.Printf("  [%s] %s\n", tag, n.Task.Name)
		fmt.Printf("    trigger: %s\n", n.Task.Trigger)
		fmt.Printf("    repo: %s | script: %s\n", n.Task.Execute.Repo, n.Task.Execute.Script)
		fmt.Printf("    reason: %s\n", n.TriggeredReason)
		if len(n.Task.DependsOn) > 0 {
			fmt.Printf("    depends_on: %s\n", strings.Join(n.Task.DependsOn, ", "))
		}
	}

	if len(proj.Variables) > 0 {
		fmt.Printf("\nVariables (%d):\n", len(proj.Variables))
		for _, v := range proj.Variables {
			scope := "project"
			if v.Scope != "" {
				scope = v.Scope
			}
			extra := ""
			if v.Repo != "" {
				extra += " repo=" + v.Repo
			}
			if v.Job != "" {
				extra += " job=" + v.Job
			}
			if v.Masked {
				extra += " [masked]"
			}
			if v.Protected {
				extra += " [protected]"
			}
			fmt.Printf("  [%s] %s = %s%s\n", scope, v.Key, v.Value, extra)
		}
	}
}

func cmdValidate(args []string) {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: pipeline file required")
		os.Exit(2)
	}
	path := fs.Arg(0)

	proj, err := parser.ParseFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		os.Exit(1)
	}

	engine := dag.New(proj)
	if err := engine.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "validation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("OK — project %q validated\n", proj.Name)
	fmt.Printf("  repos: %d\n", len(proj.Repos))
	fmt.Printf("  tasks: %d\n", len(proj.Tasks))
	if len(proj.Variables) > 0 {
		fmt.Printf("  variables: %d\n", len(proj.Variables))
	}
}

func cmdVariables(args []string) {
	fs := flag.NewFlagSet("variables", flag.ExitOnError)
	repoFlag := fs.String("repo", "", "show variables for this repo scope")
	jobFlag := fs.String("job", "", "show variables for this job scope")
	envFlag := fs.String("env", "", "filter by environment")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: pipeline file required")
		os.Exit(2)
	}
	path := fs.Arg(0)

	proj, err := parser.ParseFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		os.Exit(1)
	}

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

	resolved := store.Get(*repoFlag, *jobFlag, *envFlag)

	fmt.Printf("Project: %s\n", proj.Name)
	context := "project-wide"
	if *repoFlag != "" {
		context = "repo=" + *repoFlag
	}
	if *jobFlag != "" {
		context += " job=" + *jobFlag
	}
	if *envFlag != "" {
		context += " env=" + *envFlag
	}
	fmt.Printf("Context: %s\n", context)
	fmt.Printf("Resolved variables (%d):\n", len(resolved))
	for k, v := range resolved {
		fmt.Printf("  %s = %s\n", k, v)
	}
}

func changedSummary(changed map[string]bool) string {
	var parts []string
	for name, v := range changed {
		if v {
			parts = append(parts, name)
		}
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, ", ")
}
