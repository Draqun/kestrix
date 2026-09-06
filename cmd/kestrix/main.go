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
	"os"
	"strings"
	"time"

	"kestrix/internal/config"
	"kestrix/internal/dag"
	"kestrix/internal/parser"
	"kestrix/internal/runner"
	"kestrix/internal/variables"
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
	case "variables":
		cmdVariables(os.Args[2:])
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
  kestrix variables  <file.tf> [--repo auth_service] [--job deploy] [--env staging]`)
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
	dockerRegistry := fs.String("docker-registry", "", "Docker registry URL (env: DOCKER_REGISTRY)")
	dockerImage := fs.String("docker-image", "", "Docker image name (env: DOCKER_IMAGE)")
	dockerUsername := fs.String("docker-username", "", "Docker registry username (env: DOCKER_USERNAME)")
	dockerPassword := fs.String("docker-password", "", "Docker registry password (env: DOCKER_PASSWORD)")
	sourceCacheDir := fs.String("source-cache-dir", "", "Git object cache directory (env: KESTRIX_SOURCE_CACHE_DIR)")
	sourceCacheMaxAge := fs.Duration("source-cache-max-age", 0, "maximum age of an unused Git cache entry (env: KESTRIX_SOURCE_CACHE_MAX_AGE)")
	sourceCacheMaxBytes := fs.Int64("source-cache-max-bytes", 0, "maximum total Git cache bytes (env: KESTRIX_SOURCE_CACHE_MAX_BYTES)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	env := config.Current()

	// Load config from file (defaults if missing).
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.ApplySourceCacheEnvironment(env); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
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
	cfg.ApplySourceCacheFlags(*sourceCacheDir, *sourceCacheMaxAge, *sourceCacheMaxBytes)
	if err := cfg.SourceCache.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	// Resolve secrets from env.
	if *secret == "" {
		*secret = env.WebhookSecret
	}
	if *dispatchFlag == "" {
		*dispatchFlag = env.DispatchProject
	}
	if *gitlabToken == "" {
		*gitlabToken = env.GitLabToken
	}
	if *dbURL == "" {
		*dbURL = env.DatabaseURL
	}
	if *dbURL == "" {
		*dbURL = defaultLocalDatabaseURL
	}
	if *dockerRegistry == "" {
		*dockerRegistry = env.Docker.Registry
	}
	if *dockerImage == "" {
		*dockerImage = env.Docker.Image
	}
	if *dockerUsername == "" {
		*dockerUsername = env.Docker.Username
	}
	if *dockerPassword == "" {
		*dockerPassword = env.Docker.Password
	}

	// Apply env secrets to config.
	if *secret != "" {
		cfg.Webhook.Secret = *secret
	}
	if err := cfg.ApplyWebhookEnvironment(env); err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	if *gitlabURL != "" {
		cfg.Dispatch.GitLabURL = *gitlabURL
	}
	if *gitlabToken != "" {
		cfg.Dispatch.GitLabToken = *gitlabToken
	}
	if *dispatchFlag != "" {
		cfg.Dispatch.Enabled = true
		fmt.Sscanf(*dispatchFlag, "%d", &cfg.Dispatch.ProjectID)
	}
	if *branchFlag != "" {
		cfg.Branch = *branchFlag
	}

	// Apply CLI flags (non-empty overrides config).
	if *addr != "" {
		cfg.ListenAddr = *addr
	}
	if *dir != "" {
		cfg.Pipeline.Dir = *dir
	}
	if *method != "" {
		// method is used for SignatureConfig
	}
	if *dockerRegistry != "" {
		cfg.Docker.Registry = *dockerRegistry
	}
	if *dockerImage != "" {
		cfg.Docker.Image = *dockerImage
	}
	if *dockerUsername != "" {
		cfg.Docker.Username = *dockerUsername
	}
	if *dockerPassword != "" {
		cfg.Docker.Password = *dockerPassword
	}

	// Convert to web.Config.
	webCfg := web.Config{
		ListenAddr:    cfg.ListenAddr,
		PipelineDir:   cfg.Pipeline.Dir,
		StaticDir:     *staticDir,
		DatabaseURL:   *dbURL,
		MasterKeyPath: env.MasterKeyPath,
		Branch:        cfg.Branch,
		SourceCache: web.SourceCacheConfig{
			Directory: cfg.SourceCache.Directory,
			MaxAge:    time.Duration(cfg.SourceCache.MaxAge),
			MaxBytes:  cfg.SourceCache.MaxBytes,
		},
		Docker: web.DockerConfig{
			Registry: cfg.Docker.Registry,
			Image:    cfg.Docker.Image,
			Username: cfg.Docker.Username,
			Password: cfg.Docker.Password,
		},
		Signature: web.SignatureConfig{
			Enabled: cfg.Webhook.Secret != "",
			Secret:  cfg.Webhook.Secret,
			Method:  *method,
		},
		WebhookHistoryLimit: cfg.Webhook.HistoryLimit,
		Dispatch: web.DispatchConfig{
			Enabled:     cfg.Dispatch.Enabled,
			ProjectID:   fmt.Sprintf("%d", cfg.Dispatch.ProjectID),
			GitLabURL:   cfg.Dispatch.GitLabURL,
			GitLabToken: cfg.Dispatch.GitLabToken,
		},
		Runner: web.RunnerConfig{
			Mode:     cfg.Runner.Mode,
			Executor: cfg.Runner.Executor,
		},
	}

	// Show config file location.
	fmt.Fprintf(os.Stderr, "config: %s\n", *configPath)
	if _, statErr := os.Stat(*configPath); os.IsNotExist(statErr) {
		fmt.Fprintf(os.Stderr, "config: no config file found, using defaults + CLI flags\n")
	}

	server, err := buildServer(webCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "server startup error: %v\n", err)
		os.Exit(1)
	}
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

func buildServer(cfg web.Config) (*web.Server, error) {
	return web.NewServerE(cfg)
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
