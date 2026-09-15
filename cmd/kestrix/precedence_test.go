package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"kestrix/internal/config"
	"kestrix/internal/web"
)

// precedenceSetting describes one multi-form setting end to end: the file
// YAML snippet, the environ entry, the FlagValues mutation, and how to read
// the reconciled value out of web.Config.
type precedenceSetting struct {
	name     string
	fileYAML string
	envKV    string // "" when the setting has no env form (branch)
	flag     func(*config.FlagValues)
	wantFile string
	wantEnv  string
	wantFlag string
	get      func(web.Config) string
	// runnerEnvSnapshot marks a "runner.*" setting that reaches consumers
	// only through the derived config.Environment snapshot (WithRunnerConfig),
	// not through web.Config - true for the Docker/cancel-grace hardening
	// settings, false (the default) for settings web.Config carries directly,
	// like the run-concurrency-and-retention trio.
	runnerEnvSnapshot bool
}

func precedenceSettings() []precedenceSetting {
	return []precedenceSetting{
		{
			name: config.KeyListenAddr, fileYAML: "listen_addr: ':9090'\n",
			envKV:    "KESTRIX_ADDR=:7070",
			flag:     func(f *config.FlagValues) { f.ListenAddr = ":3000" },
			wantFile: ":9090", wantEnv: ":7070", wantFlag: ":3000",
			get: func(c web.Config) string { return c.ListenAddr },
		},
		{
			name: config.KeyDatabaseURL, fileYAML: "database_url: postgres://file/db\n",
			envKV:    "DATABASE_URL=postgres://env/db",
			flag:     func(f *config.FlagValues) { f.DatabaseURL = "postgres://flag/db" },
			wantFile: "postgres://file/db", wantEnv: "postgres://env/db", wantFlag: "postgres://flag/db",
			get: func(c web.Config) string { return c.DatabaseURL },
		},
		{
			name: config.KeyPipelineDir, fileYAML: "pipeline:\n  dir: /file/pipes\n",
			envKV:    "KESTRIX_DIR=/env/pipes",
			flag:     func(f *config.FlagValues) { f.PipelineDir = "/flag/pipes" },
			wantFile: "/file/pipes", wantEnv: "/env/pipes", wantFlag: "/flag/pipes",
			get: func(c web.Config) string { return c.PipelineDir },
		},
		{
			name: config.KeyDefaultBranch, fileYAML: "default_branch: file-branch\n",
			envKV:    "",
			flag:     func(f *config.FlagValues) { f.Branch = "flag-branch" },
			wantFile: "file-branch", wantEnv: "main", wantFlag: "flag-branch",
			get: func(c web.Config) string { return c.Branch },
		},
		{
			name: config.KeyWebhookSecret, fileYAML: "webhook:\n  secret: file-secret\n",
			envKV:    "KESTRIX_WEBHOOK_SECRET=env-secret",
			flag:     func(f *config.FlagValues) { f.WebhookSecret = "flag-secret" },
			wantFile: "file-secret", wantEnv: "env-secret", wantFlag: "flag-secret",
			get: func(c web.Config) string { return c.Signature.Secret },
		},
		{
			name: config.KeyWebhookHistoryLimit, fileYAML: "webhook:\n  history_limit: 250\n",
			envKV:    "KESTRIX_WEBHOOK_HISTORY_LIMIT=42",
			flag:     nil, // no flag form; flags never touch it
			wantFile: "250", wantEnv: "42", wantFlag: "250",
			get: func(c web.Config) string { return fmt.Sprintf("%d", c.WebhookHistoryLimit) },
		},
		{
			name: config.KeyDispatchProjectID, fileYAML: "dispatch:\n  project_id: 111\n",
			envKV:    "KESTRIX_DISPATCH_PROJECT=222",
			flag:     func(f *config.FlagValues) { f.DispatchProjectRaw = "333" },
			wantFile: "111", wantEnv: "222", wantFlag: "333",
			get: func(c web.Config) string { return c.Dispatch.ProjectID },
		},
		{
			name: config.KeyDispatchGitLabURL, fileYAML: "dispatch:\n  gitlab_url: https://file.example.test\n",
			envKV:    "KESTRIX_GITLAB_URL=https://env.example.test",
			flag:     func(f *config.FlagValues) { f.GitLabURL = "https://flag.example.test" },
			wantFile: "https://file.example.test", wantEnv: "https://env.example.test", wantFlag: "https://flag.example.test",
			get: func(c web.Config) string { return c.Dispatch.GitLabURL },
		},
		{
			name: config.KeyDispatchGitLabToken, fileYAML: "dispatch:\n  gitlab_token: file-token\n",
			envKV:    "KESTRIX_GITLAB_TOKEN=env-token",
			flag:     func(f *config.FlagValues) { f.GitLabToken = "flag-token" },
			wantFile: "file-token", wantEnv: "env-token", wantFlag: "flag-token",
			get: func(c web.Config) string { return c.Dispatch.GitLabToken },
		},
		{
			name: config.KeySourceCacheDirectory, fileYAML: "source_cache:\n  directory: /file/cache\n",
			envKV:    "KESTRIX_SOURCE_CACHE_DIR=/env/cache",
			flag:     func(f *config.FlagValues) { f.SourceCacheDir = "/flag/cache" },
			wantFile: "/file/cache", wantEnv: "/env/cache", wantFlag: "/flag/cache",
			get: func(c web.Config) string { return c.SourceCache.Directory },
		},
		{
			name: config.KeySourceCacheMaxAge, fileYAML: "source_cache:\n  max_age: 48h\n",
			envKV:    "KESTRIX_SOURCE_CACHE_MAX_AGE=24h",
			flag:     func(f *config.FlagValues) { f.SourceCacheMaxAge = 2 * time.Hour },
			wantFile: (48 * time.Hour).String(), wantEnv: (24 * time.Hour).String(), wantFlag: (2 * time.Hour).String(),
			get: func(c web.Config) string { return c.SourceCache.MaxAge.String() },
		},
		{
			name: config.KeySourceCacheMaxBytes, fileYAML: "source_cache:\n  max_bytes: 2048\n",
			envKV:    "KESTRIX_SOURCE_CACHE_MAX_BYTES=1024",
			flag:     func(f *config.FlagValues) { f.SourceCacheMaxBytes = 4096 },
			wantFile: "2048", wantEnv: "1024", wantFlag: "4096",
			get: func(c web.Config) string { return fmt.Sprintf("%d", c.SourceCache.MaxBytes) },
		},
		{
			name: config.KeyRunnerDockerPIDs, fileYAML: "runner:\n  docker_pids: 100\n",
			envKV:    "KESTRIX_RUNNER_DOCKER_PIDS=101",
			flag:     nil,
			wantFile: "100", wantEnv: "101", wantFlag: "100",
			get:               func(c web.Config) string { return "" }, // runner hardening stays in env snapshot, not web.Config
			runnerEnvSnapshot: true,
		},
		{
			name: config.KeyRunnerDockerMemory, fileYAML: "runner:\n  docker_memory: 4g\n",
			envKV:    "KESTRIX_RUNNER_DOCKER_MEMORY=1g",
			flag:     nil,
			wantFile: "4g", wantEnv: "1g", wantFlag: "4g",
			get:               func(c web.Config) string { return "" },
			runnerEnvSnapshot: true,
		},
		{
			name: config.KeyRunnerDockerCPUs, fileYAML: "runner:\n  docker_cpus: '4'\n",
			envKV:    "KESTRIX_RUNNER_DOCKER_CPUS=8",
			flag:     nil,
			wantFile: "4", wantEnv: "8", wantFlag: "4",
			get:               func(c web.Config) string { return "" },
			runnerEnvSnapshot: true,
		},
		{
			name: config.KeyRunnerDockerUser, fileYAML: "runner:\n  docker_user: '1000:1000'\n",
			envKV:    "KESTRIX_RUNNER_DOCKER_USER=2000:2000",
			flag:     nil,
			wantFile: "1000:1000", wantEnv: "2000:2000", wantFlag: "1000:1000",
			get:               func(c web.Config) string { return "" },
			runnerEnvSnapshot: true,
		},
		{
			name: config.KeyRunnerDockerNetwork, fileYAML: "runner:\n  docker_network: none\n",
			envKV:    "KESTRIX_RUNNER_DOCKER_NETWORK=host",
			flag:     nil,
			wantFile: "none", wantEnv: "host", wantFlag: "none",
			get:               func(c web.Config) string { return "" },
			runnerEnvSnapshot: true,
		},
		{
			name: config.KeyRunnerCancelGrace, fileYAML: "runner:\n  cancel_grace: 45s\n",
			envKV:    "KESTRIX_RUNNER_CANCEL_GRACE=20s",
			flag:     nil,
			wantFile: (45 * time.Second).String(), wantEnv: (20 * time.Second).String(), wantFlag: (45 * time.Second).String(),
			get:               func(c web.Config) string { return "" },
			runnerEnvSnapshot: true,
		},
		{
			name: config.KeyRunnerMaxConcurrentRuns, fileYAML: "runner:\n  max_concurrent_runs: 4\n",
			envKV:    "KESTRIX_RUNNER_MAX_CONCURRENT_RUNS=5",
			flag:     nil,
			wantFile: "4", wantEnv: "5", wantFlag: "4",
			get: func(c web.Config) string { return fmt.Sprintf("%d", c.Runner.MaxConcurrentRuns) },
		},
		{
			name: config.KeyRunHistoryLimit, fileYAML: "runner:\n  run_history_limit: 500\n",
			envKV:    "KESTRIX_RUN_HISTORY_LIMIT=600",
			flag:     nil,
			wantFile: "500", wantEnv: "600", wantFlag: "500",
			get: func(c web.Config) string { return fmt.Sprintf("%d", c.RunHistoryLimit) },
		},
		{
			name: config.KeyRunnerMaxOutputBytes, fileYAML: "runner:\n  max_output_bytes: 2048\n",
			envKV:    "KESTRIX_RUNNER_MAX_OUTPUT_BYTES=4096",
			flag:     nil,
			wantFile: "2048", wantEnv: "4096", wantFlag: "2048",
			get: func(c web.Config) string { return fmt.Sprintf("%d", c.Runner.MaxOutputBytes) },
		},
	}
}

// runPipeline feeds one triple through the production reconciliation path.
// An empty fileYAML writes an empty map so no key counts as file-set.
func runPipeline(t *testing.T, fileYAML string, environ []string, flags config.FlagValues) (*config.Config, web.Config) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := fileYAML
	if content == "" {
		content = "{}\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	env := config.LoadEnvironment(environ)
	if err := cfg.ApplyEnvironment(env); err != nil {
		t.Fatalf("ApplyEnvironment: %v", err)
	}
	if err := cfg.ApplyFlags(flags); err != nil {
		t.Fatalf("ApplyFlags: %v", err)
	}
	return cfg, buildServeConfig(cfg, env, "", "")
}

// TestPrecedenceTable asserts every multi-form setting reaches web.Config
// (or the derived runner snapshot) with flag > file > env > default.
// Skipping any single Apply step breaks at least one case per setting: drop
// ApplyEnvironment and every env-only case fails; drop ApplyFlags and every
// flag case fails (verified by commenting each call out once).
func TestPrecedenceTable(t *testing.T) {
	for _, s := range precedenceSettings() {
		t.Run(s.name, func(t *testing.T) {
			isRunner := s.runnerEnvSnapshot
			flagArgs := config.FlagValues{}
			if s.flag != nil {
				s.flag(&flagArgs)
			}
			var envOnly []string
			if s.envKV != "" {
				envOnly = []string{s.envKV}
			}

			if isRunner {
				// Runner hardening lives in the derived env snapshot, not web.Config.
				assertRunner := func(t *testing.T, cfg *config.Config, want string) {
					t.Helper()
					derived := config.LoadEnvironment(nil).WithRunnerConfig(cfg)
					var got string
					switch s.name {
					case config.KeyRunnerDockerPIDs:
						got = fmt.Sprintf("%d", derived.RunnerDockerPIDs)
					case config.KeyRunnerDockerMemory:
						got = derived.RunnerDockerMemory
					case config.KeyRunnerDockerCPUs:
						got = derived.RunnerDockerCPUs
					case config.KeyRunnerDockerUser:
						got = derived.RunnerDockerUser
					case config.KeyRunnerDockerNetwork:
						got = derived.RunnerDockerNetwork
					case config.KeyRunnerCancelGrace:
						got = derived.RunnerCancelGrace.String()
					}
					if got != want {
						t.Fatalf("%s = %q, want %q", s.name, got, want)
					}
				}
				// File only.
				cfg, _ := runPipeline(t, s.fileYAML, nil, config.FlagValues{})
				assertRunner(t, cfg, s.wantFile)
				// Env only (if the setting has an env form).
				if s.envKV != "" {
					cfg, _ = runPipeline(t, "", envOnly, config.FlagValues{})
					assertRunner(t, cfg, s.wantEnv)
					// File+env → file wins.
					cfg, _ = runPipeline(t, s.fileYAML, envOnly, config.FlagValues{})
					assertRunner(t, cfg, s.wantFile)
				}
				return
			}

			// File only.
			_, wc := runPipeline(t, s.fileYAML, nil, config.FlagValues{})
			if got := s.get(wc); got != s.wantFile {
				t.Fatalf("file only: %s = %q, want %q", s.name, got, s.wantFile)
			}
			// Env only.
			if s.envKV != "" {
				_, wc = runPipeline(t, "", envOnly, config.FlagValues{})
				if got := s.get(wc); got != s.wantEnv {
					t.Fatalf("env only: %s = %q, want %q", s.name, got, s.wantEnv)
				}
				// File+env → file wins.
				_, wc = runPipeline(t, s.fileYAML, envOnly, config.FlagValues{})
				if got := s.get(wc); got != s.wantFile {
					t.Fatalf("file+env: %s = %q, want file %q", s.name, got, s.wantFile)
				}
			}
			// Flag only, env+flag, file+env+flag → flag wins.
			if s.flag != nil {
				_, wc = runPipeline(t, "", nil, flagArgs)
				if got := s.get(wc); got != s.wantFlag {
					t.Fatalf("flag only: %s = %q, want %q", s.name, got, s.wantFlag)
				}
				if s.envKV != "" {
					_, wc = runPipeline(t, "", envOnly, flagArgs)
					if got := s.get(wc); got != s.wantFlag {
						t.Fatalf("env+flag: %s = %q, want flag %q", s.name, got, s.wantFlag)
					}
					_, wc = runPipeline(t, s.fileYAML, envOnly, flagArgs)
					if got := s.get(wc); got != s.wantFlag {
						t.Fatalf("file+env+flag: %s = %q, want flag %q", s.name, got, s.wantFlag)
					}
				} else {
					_, wc = runPipeline(t, s.fileYAML, nil, flagArgs)
					if got := s.get(wc); got != s.wantFlag {
						t.Fatalf("file+flag: %s = %q, want flag %q", s.name, got, s.wantFlag)
					}
				}
			}
		})
	}
}

// TestBuildServeConfigDatabaseURLDefault pins the empty-file+empty-env+
// empty-flags wiring: with no source for database_url, web.Config carries
// the local default rather than an empty string.
func TestBuildServeConfigDatabaseURLDefault(t *testing.T) {
	_, wc := runPipeline(t, "", nil, config.FlagValues{})
	if wc.DatabaseURL != defaultLocalDatabaseURL {
		t.Fatalf("DatabaseURL = %q, want default %q", wc.DatabaseURL, defaultLocalDatabaseURL)
	}
}

// TestPrecedenceCompleteness fails when a typed Environment field or a
// config-file key exists that no table entry covers, unless it is on an
// explicit env-only / file-only allowlist. Adding a setting without its
// table row is exactly what this must block (verified by adding a dummy
// field to each struct once and watching this fail).
func TestPrecedenceCompleteness(t *testing.T) {
	covered := make(map[string]bool)
	for _, s := range precedenceSettings() {
		covered[s.name] = true
	}
	// webhook.method is env-or-flag with no yaml form; its equivalence is
	// pinned by TestResolveWebhookMethodEnvFallback, not the table.
	envAllow := map[string]bool{
		"WebhookMethod": true, "TestDatabaseURL": true,
		"MasterKeyPath": true, "JWTSigningKeyPath": true,
		"IdentityCacheTTL": true, "LoginMaxAttempts": true, "LoginWindow": true,
		"TrustProxyHeader": true, "ContentSecurityPolicy": true, "CookieSecure": true,
		"RunnerEnvPassthrough": true,
		"HTTPReadTimeout":      true, "HTTPWriteTimeout": true, "HTTPIdleTimeout": true,
		"HTTPMaxBodyBytes": true, "WebhookMaxBodyBytes": true,
	}
	envFieldToSetting := map[string]string{
		"ListenAddr": config.KeyListenAddr, "PipelineDir": config.KeyPipelineDir,
		"WebhookSecret": config.KeyWebhookSecret, "WebhookHistoryLimit": config.KeyWebhookHistoryLimit,
		"DispatchProject": config.KeyDispatchProjectID, "GitLabURL": config.KeyDispatchGitLabURL,
		"GitLabToken": config.KeyDispatchGitLabToken, "DatabaseURL": config.KeyDatabaseURL,
		"RunnerDockerPIDs": config.KeyRunnerDockerPIDs, "RunnerDockerMemory": config.KeyRunnerDockerMemory,
		"RunnerDockerCPUs": config.KeyRunnerDockerCPUs, "RunnerDockerUser": config.KeyRunnerDockerUser,
		"RunnerDockerNetwork": config.KeyRunnerDockerNetwork, "RunnerCancelGrace": config.KeyRunnerCancelGrace,
		"Directory": config.KeySourceCacheDirectory, "MaxAge": config.KeySourceCacheMaxAge, "MaxBytes": config.KeySourceCacheMaxBytes,
		"RunnerMaxConcurrentRuns": config.KeyRunnerMaxConcurrentRuns, "RunHistoryLimit": config.KeyRunHistoryLimit,
		"RunnerMaxOutputBytes": config.KeyRunnerMaxOutputBytes,
	}

	var missing []string
	envType := reflect.TypeOf(config.Environment{})
	for i := 0; i < envType.NumField(); i++ {
		f := envType.Field(i)
		if !f.IsExported() {
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Struct && ft.Name() == "SourceCacheEnvironment" {
			for j := 0; j < ft.NumField(); j++ {
				sf := ft.Field(j)
				setting := envFieldToSetting[sf.Name]
				if setting == "" || (!covered[setting] && !envAllow[sf.Name]) {
					missing = append(missing, "Environment.SourceCache."+sf.Name)
				}
			}
			continue
		}
		if ft.Name() == "SourceCacheEnvironment" || strings.HasSuffix(ft.String(), "SourceCacheEnvironment") {
			continue
		}
		if setting, ok := envFieldToSetting[f.Name]; ok {
			if !covered[setting] {
				missing = append(missing, "Environment."+f.Name+" (setting "+setting+" not in table)")
			}
			continue
		}
		if !envAllow[f.Name] {
			missing = append(missing, "Environment."+f.Name)
		}
	}

	// Config yaml keys, recursively.
	fileAllow := map[string]bool{
		"runner.mode": true, "dispatch.enabled": true,
	}
	var walkConfig func(rt reflect.Type, prefix string, out *[]string)
	walkConfig = func(rt reflect.Type, prefix string, out *[]string) {
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			tag := f.Tag.Get("yaml")
			tagName := strings.Split(tag, ",")[0]
			if tagName == "" || tagName == "-" {
				continue
			}
			full := tagName
			if prefix != "" {
				full = prefix + "." + tagName
			}
			ft := f.Type
			if ft.Kind() == reflect.Struct && ft.Name() != "Duration" {
				walkConfig(ft, full, out)
				continue
			}
			*out = append(*out, full)
		}
	}
	var keys []string
	walkConfig(reflect.TypeOf(config.Config{}), "", &keys)
	for _, k := range keys {
		if !covered[k] && !fileAllow[k] {
			missing = append(missing, "config-file key "+k)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("uncovered configuration forms:\n  %s\nadd a table entry or an explicit allowlist line", strings.Join(missing, "\n  "))
	}
}
