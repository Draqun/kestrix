package config

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestDurationRejectsInvalidYAML(t *testing.T) {
	var duration Duration
	if err := duration.UnmarshalYAML(&yaml.Node{Value: "not-a-duration"}); err == nil {
		t.Fatal("expected invalid duration error")
	}
}

func TestDefaultConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.Branch != "main" {
		t.Errorf("Branch = %q", cfg.Branch)
	}
	if cfg.Dispatch.GitLabURL != "https://gitlab.com" {
		t.Errorf("GitLabURL = %q", cfg.Dispatch.GitLabURL)
	}
	if cfg.SourceCache.Directory == "" || time.Duration(cfg.SourceCache.MaxAge) <= 0 || cfg.SourceCache.MaxBytes <= 0 {
		t.Fatalf("invalid source-cache defaults: %+v", cfg.SourceCache)
	}
}

func TestSourceCacheConfigPrecedenceAndValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	fileCache := filepath.Join(dir, "file-cache")
	if err := os.WriteFile(path, []byte("source_cache:\n  directory: "+fileCache+"\n  max_age: 48h\n  max_bytes: 2048\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	env := LoadEnvironment([]string{
		"KESTRIX_SOURCE_CACHE_DIR=" + filepath.Join(dir, "env-cache"),
		"KESTRIX_SOURCE_CACHE_MAX_AGE=24h",
		"KESTRIX_SOURCE_CACHE_MAX_BYTES=1024",
	})
	if err := cfg.ApplyEnvironment(env); err != nil {
		t.Fatal(err)
	}
	if cfg.SourceCache.Directory != fileCache || time.Duration(cfg.SourceCache.MaxAge) != 48*time.Hour || cfg.SourceCache.MaxBytes != 2048 {
		t.Fatalf("file did not override environment: %+v", cfg.SourceCache)
	}
	cliCache := filepath.Join(dir, "cli-cache")
	if err := cfg.ApplyFlags(FlagValues{SourceCacheDir: cliCache, SourceCacheMaxAge: 2 * time.Hour, SourceCacheMaxBytes: 4096}); err != nil {
		t.Fatal(err)
	}
	if cfg.SourceCache.Directory != cliCache || time.Duration(cfg.SourceCache.MaxAge) != 2*time.Hour || cfg.SourceCache.MaxBytes != 4096 {
		t.Fatalf("CLI did not override file: %+v", cfg.SourceCache)
	}
	if err := cfg.SourceCache.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if info, err := os.Stat(cliCache); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("cache directory info=%v err=%v", info, err)
	}
}

func TestSourceCacheValidationRejectsInvalidValuesAndFilePath(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []SourceCacheConfig{
		{Directory: "", MaxAge: Duration(time.Hour), MaxBytes: 1},
		{Directory: filepath.Join(dir, "age"), MaxAge: 0, MaxBytes: 1},
		{Directory: filepath.Join(dir, "bytes"), MaxAge: Duration(time.Hour), MaxBytes: 0},
		{Directory: filePath, MaxAge: Duration(time.Hour), MaxBytes: 1},
	} {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected invalid cache config rejection: %+v", cfg)
		}
	}
}

func TestSourceCacheValidationFilesystemFailures(t *testing.T) {
	oldMkdirAll, oldStatPath, oldChmodPath := mkdirAll, statPath, chmodPath
	t.Cleanup(func() {
		mkdirAll, statPath, chmodPath = oldMkdirAll, oldStatPath, oldChmodPath
	})
	dirInfo, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	valid := SourceCacheConfig{Directory: "/cache", MaxAge: Duration(time.Hour), MaxBytes: 1}

	mkdirAll = func(string, os.FileMode) error { return nil }
	statPath = func(string) (os.FileInfo, error) { return nil, errors.New("stat failed") }
	if err := valid.Validate(); err == nil || !strings.Contains(err.Error(), "inspect") {
		t.Fatalf("stat error = %v", err)
	}

	statPath = func(string) (os.FileInfo, error) { return fileInfo, nil }
	if err := valid.Validate(); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file error = %v", err)
	}

	statPath = func(string) (os.FileInfo, error) { return dirInfo, nil }
	chmodPath = func(string, os.FileMode) error { return errors.New("chmod failed") }
	if err := valid.Validate(); err == nil || !strings.Contains(err.Error(), "secure") {
		t.Fatalf("chmod error = %v", err)
	}
}

func TestLoadMissing(t *testing.T) {
	cfg, err := Load("/nonexistent/config.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("expected defaults, got ListenAddr=%q", cfg.ListenAddr)
	}
}

func TestMasterKeyPathIsNotReadFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("master_key_path: /legacy/key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Unknown keys fail startup naming the key, never the value.
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "master_key_path") {
		t.Fatalf("Load error = %v, want it to name master_key_path", err)
	}
	cfg := DefaultConfig()
	data, err := marshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "master_key_path") {
		t.Fatalf("YAML still persisted master_key_path: %s", data)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	original := &Config{
		ListenAddr: ":9090",
		Branch:     "develop",
		Pipeline:   PipelineConfig{Dir: "/app/pipelines"},
		Webhook:    WebhookConfig{Secret: "my-secret"},
		Dispatch: DispatchConfig{
			Enabled:     true,
			ProjectID:   12345,
			GitLabURL:   "https://gitlab.example.com",
			GitLabToken: "glpat-xxx",
		},
	}

	if err := Save(path, original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want :9090", loaded.ListenAddr)
	}
	if loaded.Branch != "develop" {
		t.Errorf("Branch = %q, want develop", loaded.Branch)
	}
	if loaded.Pipeline.Dir != "/app/pipelines" {
		t.Errorf("Pipeline.Dir = %q", loaded.Pipeline.Dir)
	}
	if loaded.Webhook.Secret != "my-secret" {
		t.Errorf("Webhook.Secret = %q", loaded.Webhook.Secret)
	}
	if !loaded.Dispatch.Enabled {
		t.Error("Dispatch.Enabled = false, want true")
	}
	if loaded.Dispatch.ProjectID != 12345 {
		t.Errorf("Dispatch.ProjectID = %d, want 12345", loaded.Dispatch.ProjectID)
	}
	if loaded.Dispatch.GitLabURL != "https://gitlab.example.com" {
		t.Errorf("Dispatch.GitLabURL = %q", loaded.Dispatch.GitLabURL)
	}
	if loaded.Dispatch.GitLabToken != "glpat-xxx" {
		t.Errorf("Dispatch.GitLabToken = %q", loaded.Dispatch.GitLabToken)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("{{invalid yaml"), 0o600)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadReportsReadFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("listen_addr: ':8080'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := readFile
	readFile = func(string) ([]byte, error) { return nil, errors.New("read failed") }
	t.Cleanup(func() { readFile = old })
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("Load error = %v", err)
	}
}

func TestApplyEnvironmentBranches(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.ApplyEnvironment(nil); err != nil {
		t.Fatal(err)
	}
	invalid := LoadEnvironment([]string{"KESTRIX_SOURCE_CACHE_MAX_AGE=invalid"})
	if err := cfg.ApplyEnvironment(invalid); err == nil {
		t.Fatal("expected environment validation error")
	}
	env := LoadEnvironment([]string{
		"KESTRIX_SOURCE_CACHE_DIR=/env/cache",
		"KESTRIX_SOURCE_CACHE_MAX_AGE=12h",
		"KESTRIX_SOURCE_CACHE_MAX_BYTES=1234",
	})
	if err := cfg.ApplyEnvironment(env); err != nil {
		t.Fatal(err)
	}
	if cfg.SourceCache.Directory != "/env/cache" || time.Duration(cfg.SourceCache.MaxAge) != 12*time.Hour || cfg.SourceCache.MaxBytes != 1234 {
		t.Fatalf("source cache = %+v", cfg.SourceCache)
	}
}

func TestPresenceEmptyCountsAsSet(t *testing.T) {
	dir := t.TempDir()
	// Explicitly empty secret counts as set: env must not fill it.
	explicit := filepath.Join(dir, "explicit.yaml")
	if err := os.WriteFile(explicit, []byte("webhook:\n  secret: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(explicit)
	if err != nil {
		t.Fatal(err)
	}
	env := LoadEnvironment([]string{"KESTRIX_WEBHOOK_SECRET=env-secret"})
	if err := cfg.ApplyEnvironment(env); err != nil {
		t.Fatal(err)
	}
	if cfg.Webhook.Secret != "" {
		t.Fatalf("explicitly empty file value was overwritten by env: %q", cfg.Webhook.Secret)
	}
	// Omitted key counts as absent: env fills it.
	omitted := filepath.Join(dir, "omitted.yaml")
	if err := os.WriteFile(omitted, []byte("listen_addr: ':9090'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg2, err := Load(omitted)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg2.ApplyEnvironment(env); err != nil {
		t.Fatal(err)
	}
	if cfg2.Webhook.Secret != "env-secret" {
		t.Fatalf("omitted file value was not filled from env: %q", cfg2.Webhook.Secret)
	}
	// Explicit zero history_limit counts as set: env must not fill it,
	// and Load normalizes it to the default rather than the env value.
	zeroPath := filepath.Join(dir, "zero.yaml")
	if err := os.WriteFile(zeroPath, []byte("webhook:\n  history_limit: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg3, err := Load(zeroPath)
	if err != nil {
		t.Fatal(err)
	}
	env2 := LoadEnvironment([]string{"KESTRIX_WEBHOOK_HISTORY_LIMIT=42"})
	if err := cfg3.ApplyEnvironment(env2); err != nil {
		t.Fatal(err)
	}
	if cfg3.Webhook.HistoryLimit != DefaultWebhookHistoryLimit {
		t.Fatalf("explicit zero history_limit = %d, want default %d", cfg3.Webhook.HistoryLimit, DefaultWebhookHistoryLimit)
	}
}

// TestPresenceEmptyCountsAsSetForEveryKey proves the file-wins rule for all
// multi-form keys at once: one file with every key explicitly empty (or zero)
// plus an environment setting every one of them must leave the file values
// untouched. Only webhook.history_limit is special: an explicit zero counts
// as set, so the environment must not fill it, and Load normalizes it to the
// default.
func TestPresenceEmptyCountsAsSetForEveryKey(t *testing.T) {
	const fileYAML = `listen_addr: ""
database_url: ""
default_branch: ""
pipeline:
  dir: ""
webhook:
  secret: ""
  history_limit: 0
dispatch:
  project_id: 0
  gitlab_url: ""
  gitlab_token: ""
source_cache:
  directory: ""
  max_age: 0s
  max_bytes: 0
runner:
  docker_pids: 0
  docker_memory: ""
  docker_cpus: ""
  docker_user: ""
  docker_network: ""
  cancel_grace: 0s
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(fileYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	env := LoadEnvironment([]string{
		"KESTRIX_ADDR=:7070",
		"DATABASE_URL=postgres://env/db",
		"KESTRIX_DIR=/env/pipes",
		"KESTRIX_WEBHOOK_SECRET=env-secret",
		"KESTRIX_WEBHOOK_HISTORY_LIMIT=42",
		"KESTRIX_DISPATCH_PROJECT=222",
		"KESTRIX_GITLAB_URL=https://env.example.test",
		"KESTRIX_GITLAB_TOKEN=env-token",
		"KESTRIX_SOURCE_CACHE_DIR=/env/cache",
		"KESTRIX_SOURCE_CACHE_MAX_AGE=24h",
		"KESTRIX_SOURCE_CACHE_MAX_BYTES=1024",
		"KESTRIX_RUNNER_DOCKER_PIDS=101",
		"KESTRIX_RUNNER_DOCKER_MEMORY=1g",
		"KESTRIX_RUNNER_DOCKER_CPUS=8",
		"KESTRIX_RUNNER_DOCKER_USER=2000:2000",
		"KESTRIX_RUNNER_DOCKER_NETWORK=host",
		"KESTRIX_RUNNER_CANCEL_GRACE=20s",
	})
	if err := cfg.ApplyEnvironment(env); err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{KeyListenAddr, cfg.ListenAddr, ""},
		{KeyDatabaseURL, cfg.DatabaseURL, ""},
		{KeyDefaultBranch, cfg.Branch, ""},
		{KeyPipelineDir, cfg.Pipeline.Dir, ""},
		{KeyWebhookSecret, cfg.Webhook.Secret, ""},
		{KeyWebhookHistoryLimit, cfg.Webhook.HistoryLimit, DefaultWebhookHistoryLimit},
		{KeyDispatchProjectID, cfg.Dispatch.ProjectID, 0},
		{KeyDispatchGitLabURL, cfg.Dispatch.GitLabURL, ""},
		{KeyDispatchGitLabToken, cfg.Dispatch.GitLabToken, ""},
		{KeySourceCacheDirectory, cfg.SourceCache.Directory, ""},
		{KeySourceCacheMaxAge, time.Duration(cfg.SourceCache.MaxAge), time.Duration(0)},
		{KeySourceCacheMaxBytes, cfg.SourceCache.MaxBytes, int64(0)},
		{KeyRunnerDockerPIDs, cfg.Runner.DockerPIDs, 0},
		{KeyRunnerDockerMemory, cfg.Runner.DockerMemory, ""},
		{KeyRunnerDockerCPUs, cfg.Runner.DockerCPUs, ""},
		{KeyRunnerDockerUser, cfg.Runner.DockerUser, ""},
		{KeyRunnerDockerNetwork, cfg.Runner.DockerNetwork, ""},
		{KeyRunnerCancelGrace, time.Duration(cfg.Runner.CancelGrace), time.Duration(0)},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %#v, want %#v (env must not fill an explicitly empty file value)", c.name, c.got, c.want)
		}
	}
	if cfg.Dispatch.Enabled {
		t.Error("dispatch enabled by an explicitly zero project_id with env set")
	}
}

func TestDefaultConfigRunnerDockerDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Runner.DockerPIDs != 256 {
		t.Errorf("DockerPIDs = %d, want 256", cfg.Runner.DockerPIDs)
	}
	if cfg.Runner.DockerMemory != "2g" {
		t.Errorf("DockerMemory = %q, want 2g", cfg.Runner.DockerMemory)
	}
	if cfg.Runner.DockerCPUs != "2" {
		t.Errorf("DockerCPUs = %q, want 2", cfg.Runner.DockerCPUs)
	}
	if cfg.Runner.DockerUser != "" {
		t.Errorf("DockerUser = %q, want empty", cfg.Runner.DockerUser)
	}
	if cfg.Runner.DockerNetwork != "bridge" {
		t.Errorf("DockerNetwork = %q, want bridge", cfg.Runner.DockerNetwork)
	}
	if time.Duration(cfg.Runner.CancelGrace) != 10*time.Second {
		t.Errorf("CancelGrace = %v, want 10s", time.Duration(cfg.Runner.CancelGrace))
	}
}

func TestRunnerConfigPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("runner:\n  docker_pids: 100\n  docker_memory: 4g\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	env := LoadEnvironment([]string{
		"KESTRIX_RUNNER_DOCKER_PIDS=999",
		"KESTRIX_RUNNER_DOCKER_MEMORY=1g",
		"KESTRIX_RUNNER_DOCKER_CPUS=4",
		"KESTRIX_RUNNER_DOCKER_USER=1000:1000",
		"KESTRIX_RUNNER_DOCKER_NETWORK=none",
		"KESTRIX_RUNNER_CANCEL_GRACE=20s",
	})
	if err := cfg.ApplyEnvironment(env); err != nil {
		t.Fatal(err)
	}
	// File explicitly set docker_pids/docker_memory: they win over env.
	if cfg.Runner.DockerPIDs != 100 {
		t.Errorf("DockerPIDs = %d, want 100 (file wins)", cfg.Runner.DockerPIDs)
	}
	if cfg.Runner.DockerMemory != "4g" {
		t.Errorf("DockerMemory = %q, want 4g (file wins)", cfg.Runner.DockerMemory)
	}
	// Not set in the file: environment fills in.
	if cfg.Runner.DockerCPUs != "4" {
		t.Errorf("DockerCPUs = %q, want 4 (env wins)", cfg.Runner.DockerCPUs)
	}
	if cfg.Runner.DockerUser != "1000:1000" {
		t.Errorf("DockerUser = %q, want 1000:1000 (env wins)", cfg.Runner.DockerUser)
	}
	if cfg.Runner.DockerNetwork != "none" {
		t.Errorf("DockerNetwork = %q, want none (env wins)", cfg.Runner.DockerNetwork)
	}
	if time.Duration(cfg.Runner.CancelGrace) != 20*time.Second {
		t.Errorf("CancelGrace = %v, want 20s (env wins)", time.Duration(cfg.Runner.CancelGrace))
	}
}

func TestApplyEnvironmentRunnerBranches(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.ApplyEnvironment(nil); err != nil {
		t.Fatal(err)
	}
	invalid := LoadEnvironment([]string{"KESTRIX_RUNNER_DOCKER_PIDS=-1"})
	if err := cfg.ApplyEnvironment(invalid); err == nil {
		t.Fatal("expected environment validation error")
	}

	env := LoadEnvironment([]string{
		"KESTRIX_RUNNER_DOCKER_PIDS=128",
		"KESTRIX_RUNNER_DOCKER_MEMORY=1g",
	})
	if err := cfg.ApplyEnvironment(env); err != nil {
		t.Fatal(err)
	}
	if cfg.Runner.DockerPIDs != 128 {
		t.Errorf("DockerPIDs = %d, want 128 (no file presence)", cfg.Runner.DockerPIDs)
	}
	if cfg.Runner.DockerMemory != "1g" {
		t.Errorf("DockerMemory = %q, want 1g (no file presence)", cfg.Runner.DockerMemory)
	}
}

func TestValidateRunnerBoundsRejectsEachNonPositiveSetting(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"max_concurrent_runs", func(c *Config) { c.Runner.MaxConcurrentRuns = 0 }, "runner.max_concurrent_runs"},
		{"run_history_limit", func(c *Config) { c.Runner.RunHistoryLimit = -1 }, "runner.run_history_limit"},
		{"max_output_bytes", func(c *Config) { c.Runner.MaxOutputBytes = 0 }, "runner.max_output_bytes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tc.mutate(cfg)
			err := cfg.validateRunnerBounds()
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to name %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// A non-positive value set in the config file is not caught by
// LoadEnvironment's own validation (which only ever sees env-var strings),
// so ApplyEnvironment must still reject it via validateRunnerBounds once the
// file's value has been reconciled - see runtime-configuration spec.md
// "Invalid value".
func TestApplyEnvironmentRejectsNonPositiveRunHistoryLimitFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("runner:\n  run_history_limit: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ApplyEnvironment(LoadEnvironment(nil)); err == nil {
		t.Fatal("expected an error for a zero run history limit set in the file")
	} else if !strings.Contains(err.Error(), "runner.run_history_limit") {
		t.Errorf("error = %q, want it to name runner.run_history_limit", err.Error())
	}
}

func TestApplyFlags(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.ApplyFlags(FlagValues{
		ListenAddr: ":3000", PipelineDir: "/opt/pipelines", Branch: "release",
		GitLabToken: "glpat-yep", DispatchProjectRaw: "99999",
	}); err != nil {
		t.Fatal(err)
	}

	if cfg.ListenAddr != ":3000" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.Pipeline.Dir != "/opt/pipelines" {
		t.Errorf("Pipeline.Dir = %q", cfg.Pipeline.Dir)
	}
	if cfg.Branch != "release" {
		t.Errorf("Branch = %q", cfg.Branch)
	}
	if cfg.Dispatch.GitLabToken != "glpat-yep" {
		t.Errorf("GitLabToken = %q", cfg.Dispatch.GitLabToken)
	}
	if !cfg.Dispatch.Enabled {
		t.Error("Enabled should be true")
	}
	if cfg.Dispatch.ProjectID != 99999 {
		t.Errorf("ProjectID = %d", cfg.Dispatch.ProjectID)
	}
}

func TestApplyFlagsEmpty(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.ApplyFlags(FlagValues{}); err != nil {
		t.Fatal(err)
	}

	if cfg.ListenAddr != ":8080" {
		t.Errorf("empty ApplyFlags changed ListenAddr to %q", cfg.ListenAddr)
	}
	if cfg.Branch != "main" {
		t.Errorf("empty ApplyFlags changed Branch to %q", cfg.Branch)
	}
}

func TestApplyFlagsRejectsInvalidDispatch(t *testing.T) {
	// Garbage, prefixed, zero, and negative values fail startup naming the
	// flag without echoing the value; dispatch stays off.
	for _, raw := range []string{"abc", "333xyz", "0", "-5"} {
		cfg := DefaultConfig()
		err := cfg.ApplyFlags(FlagValues{DispatchProjectRaw: raw})
		if err == nil {
			t.Fatalf("--dispatch %q: expected startup error", raw)
		}
		if !strings.Contains(err.Error(), "--dispatch") {
			t.Errorf("--dispatch %q: error = %v, want it to name the flag", raw, err)
		}
		if strings.Contains(err.Error(), raw) {
			t.Errorf("--dispatch %q: error = %v, must not echo the value", raw, err)
		}
		if cfg.Dispatch.Enabled {
			t.Errorf("--dispatch %q: dispatch enabled despite invalid value", raw)
		}
	}
	// Empty means unset: no error, dispatch stays off.
	cfg := DefaultConfig()
	if err := cfg.ApplyFlags(FlagValues{}); err != nil {
		t.Fatal(err)
	}
	if cfg.Dispatch.Enabled {
		t.Error("empty ApplyFlags enabled dispatch")
	}
}

// TestApplyFlagsInvalidDispatchAppliesNothing proves ApplyFlags is atomic:
// an invalid --dispatch value alongside other flags fails before any field
// is mutated, not partway through.
func TestApplyFlagsInvalidDispatchAppliesNothing(t *testing.T) {
	cfg := DefaultConfig()
	before := *cfg
	err := cfg.ApplyFlags(FlagValues{
		ListenAddr:         ":3000",
		PipelineDir:        "/flag/pipes",
		WebhookSecret:      "flag-secret",
		DatabaseURL:        "postgres://flag/db",
		GitLabURL:          "https://flag.example.test",
		GitLabToken:        "flag-token",
		DispatchProjectRaw: "not-a-number",
	})
	if err == nil {
		t.Fatal("expected startup error")
	}
	if !reflect.DeepEqual(*cfg, before) {
		t.Fatalf("ApplyFlags mutated config despite invalid dispatch value: got %+v, want %+v", *cfg, before)
	}
}

// TestApplyEnvironmentInvalidDispatchAppliesNothing is the ApplyEnvironment
// counterpart: an invalid KESTRIX_DISPATCH_PROJECT alongside other env vars
// fails before any field is mutated.
func TestApplyEnvironmentInvalidDispatchAppliesNothing(t *testing.T) {
	cfg := DefaultConfig()
	before := *cfg
	env := LoadEnvironment([]string{
		"KESTRIX_ADDR=:3000",
		"KESTRIX_DIR=/env/pipes",
		"KESTRIX_WEBHOOK_SECRET=env-secret",
		"DATABASE_URL=postgres://env/db",
		"KESTRIX_GITLAB_URL=https://env.example.test",
		"KESTRIX_GITLAB_TOKEN=env-token",
		"KESTRIX_DISPATCH_PROJECT=not-a-number",
	})
	err := cfg.ApplyEnvironment(env)
	if err == nil {
		t.Fatal("expected startup error")
	}
	if !reflect.DeepEqual(*cfg, before) {
		t.Fatalf("ApplyEnvironment mutated config despite invalid dispatch value: got %+v, want %+v", *cfg, before)
	}
}

func TestApplyFlagsBeatsFileBeatsEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("listen_addr: ':9090'\nsource_cache:\n  max_bytes: 2048\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	newCfg := func() *Config {
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		env := LoadEnvironment([]string{
			"KESTRIX_ADDR=:7070",
			"KESTRIX_DISPATCH_PROJECT=111",
			"KESTRIX_SOURCE_CACHE_MAX_AGE=24h",
		})
		if err := cfg.ApplyEnvironment(env); err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	// String: file beats env.
	if cfg := newCfg(); cfg.ListenAddr != ":9090" {
		t.Fatalf("string: ListenAddr = %q, want file :9090 over env :7070", cfg.ListenAddr)
	}
	// Flag beats file.
	cfg := newCfg()
	if err := cfg.ApplyFlags(FlagValues{ListenAddr: ":3000"}); err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != ":3000" {
		t.Fatalf("string: ListenAddr = %q, want flag :3000", cfg.ListenAddr)
	}
	// Int: env fills absent, flag beats env.
	cfg = newCfg()
	if cfg.Dispatch.ProjectID != 111 {
		t.Fatalf("int: ProjectID = %d, want env 111", cfg.Dispatch.ProjectID)
	}
	if err := cfg.ApplyFlags(FlagValues{DispatchProjectRaw: "222"}); err != nil {
		t.Fatal(err)
	}
	if cfg.Dispatch.ProjectID != 222 {
		t.Fatalf("int: ProjectID = %d, want flag 222", cfg.Dispatch.ProjectID)
	}
	// Duration: env fills absent key.
	cfg = newCfg()
	if time.Duration(cfg.SourceCache.MaxAge) != 24*time.Hour {
		t.Fatalf("duration: MaxAge = %v, want env 24h", time.Duration(cfg.SourceCache.MaxAge))
	}
	envPath := filepath.Join(dir, "env.yaml")
	if err := os.WriteFile(envPath, []byte("listen_addr: ':8080'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgEnv, err := Load(envPath)
	if err != nil {
		t.Fatal(err)
	}
	envOnly := LoadEnvironment([]string{"KESTRIX_SOURCE_CACHE_MAX_AGE=24h"})
	if err := cfgEnv.ApplyEnvironment(envOnly); err != nil {
		t.Fatal(err)
	}
	if time.Duration(cfgEnv.SourceCache.MaxAge) != 24*time.Hour {
		t.Fatalf("duration: MaxAge = %v, want env 24h", time.Duration(cfgEnv.SourceCache.MaxAge))
	}
	if err := cfgEnv.ApplyFlags(FlagValues{SourceCacheMaxAge: 2 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	if time.Duration(cfgEnv.SourceCache.MaxAge) != 2*time.Hour {
		t.Fatalf("duration: MaxAge = %v, want flag 2h", time.Duration(cfgEnv.SourceCache.MaxAge))
	}
}

// DefaultDir hits os.UserHomeDir + MkdirAll. We can't easliy fake
// os.UserHomeDir without injecting a HOME override, so we exercise
// the happy path and trust that mkdir/fail surfaces through fmt.Errorf.
func TestDefaultDirHappyPath(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	want := filepath.Join(tmpHome, ".kestrix")
	if dir != want {
		t.Errorf("DefaultDir = %q, want %q", dir, want)
	}
	// The directory should actually exist as a side-effect.
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("DefaultDir didn't actually create the directory: %v", err)
	}
}

// Unsetting HOME causes UserHomeDir to return an error — that
// triggers DefaultDir's first err branch.
func TestDefaultDirUnsetHome(t *testing.T) {
	// Setting HOME="" matches what os.UserHomeDir codes to fail on.
	// On Linux, UserHomeDir returns an error only when HOME is not set.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	// Then unset it AFTER t.Setenv's deferred restore has been copied.
	os.Unsetenv("HOME")
	defer os.Setenv("HOME", tmpHome)

	if _, err := DefaultDir(); err == nil {
		t.Fatal("expected DefaultDir to fail when HOME is unset")
	}
}

func TestDefaultPath(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	want := filepath.Join(tmpHome, ".kestrix", "config.yaml")
	if got := DefaultPath(); got != want {
		t.Errorf("DefaultPath = %q, want %q", got, want)
	}
}

// DefaultPath's empty-home fallback: when os.UserHomeDir fails, the
// function substitutes "." and continues. That branch isn't hit on
// Linux unless HOME is unset manually (HOME="" still maps to the
// current user via /etc/passwd).
func TestDefaultPathEmptyHomeFallback(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	os.Unsetenv("HOME")
	defer os.Setenv("HOME", tmpHome)

	got := DefaultPath()
	if got == "" {
		t.Fatalf("DefaultPath returned empty string")
	}
	// Should contain the .kestrix/config.yaml fallback.
	if filepath.Base(got) != "config.yaml" {
		t.Errorf("DefaultPath = %q, expected basename config.yaml", got)
	}
}

// Load on a path that exists but is a directory triggers a non-IsNotExist
// error from os.ReadFile, exercising the second branch in Load.
func TestLoadNonExistentError(t *testing.T) {
	// A directory, not a file: readFile fails with EISDIR on every platform.
	// (/proc did not exist on macOS, so the test silently took the IsNotExist path.)
	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("expected error for non-existent config in non-IsNotExist branch")
	}
}

// --- Unreachable branches in DefaultDir / Save / DefaultPath -----------
//
// The following branches cannot be exercised without artificially
// failing OS calls (permission denial on MkdirAll, yaml.Marshal
// error, env-based UserHomeDir error other than HOME=empty). Production
// code keeps them as defensive guards; we document them here rather
// than fake it (per the "annotate, don't fake" rule from
// go-coverage-to-100):
//
//   - DefaultDir MkdirAll err (L87): cannot reproduce without a read-only
//     parent directory; running as root in CI defeats it.
//   - Save MkdirAll err (L124): same reasoning as above.
//   - Save yaml.Marshal err (L129): impossible for *Config because none
//     of its fields are func / chan / unsupported interface.
//   - DefaultPath when UserHomeDir returns error but Go still keeps the
//     empty string (L96): HOME= covers most platforms, but Linux
//     falls through to /etc/passwd when HOME is missing entirely,
//
// Coverage notes (in-tree): these four branches are listed in the
// coverage report and explicitly tagged "unreachable in normal use".

// Save should still work even when the path lies in an existing
// directory (MkdirAll is a no-op).
func TestSaveIntoExistingDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.yaml") // nested doesn't exist yet
	if err := Save(path, DefaultConfig()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not written: %v", err)
	}
}

func TestDefaultDirMkdirErrorMock(t *testing.T) {
	oldHome := userHomeDir
	oldMkdirAll := mkdirAll
	t.Cleanup(func() {
		userHomeDir = oldHome
		mkdirAll = oldMkdirAll
	})

	userHomeDir = func() (string, error) { return "/home/alice", nil }
	mkdirAll = func(string, os.FileMode) error { return errors.New("boom") }

	got, err := DefaultDir()
	if err == nil || got != "" || !strings.Contains(err.Error(), "create /home/alice/.kestrix") {
		t.Fatalf("DefaultDir = (%q, %v)", got, err)
	}
}

func TestSaveCreateDirErrorMock(t *testing.T) {
	oldMkdirAll := mkdirAll
	oldMarshal := marshalYAML
	oldWrite := writeFile
	t.Cleanup(func() {
		mkdirAll = oldMkdirAll
		marshalYAML = oldMarshal
		writeFile = oldWrite
	})

	mkdirAll = func(string, os.FileMode) error { return errors.New("mkdir boom") }
	marshalYAML = func(any) ([]byte, error) { t.Fatal("marshal should not be called"); return nil, nil }
	writeFile = func(string, []byte, os.FileMode) error { t.Fatal("write should not be called"); return nil }

	if err := Save("/tmp/config.yaml", DefaultConfig()); err == nil || !strings.Contains(err.Error(), "create dir") {
		t.Fatalf("Save error = %v", err)
	}
}

func TestSaveMarshalErrorMock(t *testing.T) {
	oldMkdirAll := mkdirAll
	oldMarshal := marshalYAML
	oldWrite := writeFile
	t.Cleanup(func() {
		mkdirAll = oldMkdirAll
		marshalYAML = oldMarshal
		writeFile = oldWrite
	})

	mkdirAll = func(string, os.FileMode) error { return nil }
	marshalYAML = func(any) ([]byte, error) { return nil, errors.New("marshal boom") }
	writeFile = func(string, []byte, os.FileMode) error { t.Fatal("write should not be called"); return nil }

	if err := Save("/tmp/config.yaml", DefaultConfig()); err == nil || !strings.Contains(err.Error(), "marshal config") {
		t.Fatalf("Save error = %v", err)
	}
}

func TestWebhookHistoryLimitDefault(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Webhook.HistoryLimit != DefaultWebhookHistoryLimit {
		t.Fatalf("default history limit = %d, want %d", cfg.Webhook.HistoryLimit, DefaultWebhookHistoryLimit)
	}
}

func TestLoadNormalizesWebhookHistoryLimit(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want int
	}{
		{"absent", "listen_addr: \":9090\"\n", DefaultWebhookHistoryLimit},
		{"explicit", "webhook:\n  history_limit: 250\n", 250},
		{"zero falls back", "webhook:\n  history_limit: 0\n", DefaultWebhookHistoryLimit},
		{"negative falls back", "webhook:\n  history_limit: -5\n", DefaultWebhookHistoryLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Webhook.HistoryLimit != tc.want {
				t.Errorf("history limit = %d, want %d", cfg.Webhook.HistoryLimit, tc.want)
			}
		})
	}
}

func TestApplyEnvironmentFileWinsOverEnv(t *testing.T) {
	// File-set keys win; file-absent keys fill from env; both-absent keeps default.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("webhook:\n  secret: file-secret\n  history_limit: 250\ndispatch:\n  project_id: 111\n  gitlab_url: https://file.example.test\n  gitlab_token: file-token\nlisten_addr: ':9090'\ndatabase_url: postgres://file/db\npipeline:\n  dir: /file/pipes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	env := LoadEnvironment([]string{
		"KESTRIX_ADDR=:7070",
		"KESTRIX_DIR=/env/pipes",
		"KESTRIX_WEBHOOK_SECRET=env-secret",
		"KESTRIX_WEBHOOK_HISTORY_LIMIT=42",
		"KESTRIX_DISPATCH_PROJECT=222",
		"KESTRIX_GITLAB_URL=https://env.example.test",
		"KESTRIX_GITLAB_TOKEN=env-token",
		"DATABASE_URL=postgres://env/db",
	})
	if err := cfg.ApplyEnvironment(env); err != nil {
		t.Fatal(err)
	}
	if cfg.Webhook.Secret != "file-secret" {
		t.Errorf("secret = %q, want file", cfg.Webhook.Secret)
	}
	if cfg.Webhook.HistoryLimit != 250 {
		t.Errorf("history_limit = %d, want file 250", cfg.Webhook.HistoryLimit)
	}
	if cfg.Dispatch.ProjectID != 111 {
		t.Errorf("project_id = %d, want file 111", cfg.Dispatch.ProjectID)
	}
	if cfg.Dispatch.GitLabURL != "https://file.example.test" {
		t.Errorf("gitlab_url = %q, want file", cfg.Dispatch.GitLabURL)
	}
	if cfg.Dispatch.GitLabToken != "file-token" {
		t.Errorf("gitlab_token = %q, want file", cfg.Dispatch.GitLabToken)
	}
	if cfg.ListenAddr != ":9090" {
		t.Errorf("listen_addr = %q, want file", cfg.ListenAddr)
	}
	if cfg.DatabaseURL != "postgres://file/db" {
		t.Errorf("database_url = %q, want file", cfg.DatabaseURL)
	}
	if cfg.Pipeline.Dir != "/file/pipes" {
		t.Errorf("pipeline.dir = %q, want file", cfg.Pipeline.Dir)
	}

	// File-absent + env-set → env wins.
	path2 := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(path2, []byte("listen_addr: ':8080'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg2, err := Load(path2)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg2.ApplyEnvironment(env); err != nil {
		t.Fatal(err)
	}
	if cfg2.Webhook.Secret != "env-secret" {
		t.Errorf("secret = %q, want env", cfg2.Webhook.Secret)
	}
	if cfg2.Webhook.HistoryLimit != 42 {
		t.Errorf("history_limit = %d, want env 42", cfg2.Webhook.HistoryLimit)
	}
	if cfg2.Dispatch.ProjectID != 222 {
		t.Errorf("project_id = %d, want env 222", cfg2.Dispatch.ProjectID)
	}
	if cfg2.Pipeline.Dir != "/env/pipes" {
		t.Errorf("pipeline.dir = %q, want env", cfg2.Pipeline.Dir)
	}
	if cfg2.DatabaseURL != "postgres://env/db" {
		t.Errorf("database_url = %q, want env", cfg2.DatabaseURL)
	}

	// Both absent → default kept.
	cfg3, err := Load(filepath.Join(dir, "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// Load on missing file returns defaults; Apply empty env keeps them.
	if err := cfg3.ApplyEnvironment(LoadEnvironment(nil)); err != nil {
		t.Fatal(err)
	}
	if cfg3.ListenAddr != ":8080" {
		t.Errorf("listen_addr = %q, want default :8080", cfg3.ListenAddr)
	}
	if cfg3.Webhook.HistoryLimit != DefaultWebhookHistoryLimit {
		t.Errorf("history_limit = %d, want default", cfg3.Webhook.HistoryLimit)
	}

	t.Run("nil environment is a no-op", func(t *testing.T) {
		cfg := DefaultConfig()
		if err := cfg.ApplyEnvironment(nil); err != nil {
			t.Fatalf("ApplyEnvironment: %v", err)
		}
	})

	for _, raw := range []string{"0", "-1", "many"} {
		t.Run("invalid "+raw, func(t *testing.T) {
			cfg := DefaultConfig()
			env := LoadEnvironment([]string{"KESTRIX_WEBHOOK_HISTORY_LIMIT=" + raw})
			err := cfg.ApplyEnvironment(env)
			if err == nil {
				t.Fatal("expected an error for an invalid history limit")
			}
			if !strings.Contains(err.Error(), "KESTRIX_WEBHOOK_HISTORY_LIMIT") {
				t.Errorf("error = %v, want it to name the variable", err)
			}
		})
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	const secretValue = "super-secret-value-12345"
	cases := []struct {
		name     string
		yaml     string
		want     string
		hint     string
		canaries []string
	}{
		{"top-level", "bogus_key: 1\nwebhook:\n  secret: " + secretValue + "\n", "bogus_key", "", []string{secretValue}},
		{"nested runner.executor", "runner:\n  executor: canary-executor-9z\nwebhook:\n  secret: " + secretValue + "\n", "executor", "had no effect", []string{"canary-executor-9z", secretValue}},
		{"docker block", "docker:\n  registry: canary-registry-9z\nwebhook:\n  secret: " + secretValue + "\n", "docker", "KESTRIX_RUNNER_ENV_PASSTHROUGH", []string{"canary-registry-9z", secretValue}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if err == nil {
				t.Fatal("expected unknown-key error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to name %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error = %v, want it to name the file path", err)
			}
			for _, canary := range tc.canaries {
				if strings.Contains(err.Error(), canary) {
					t.Errorf("error = %v, must not echo file value %q", err, canary)
				}
			}
			if tc.hint != "" && !strings.Contains(err.Error(), tc.hint) {
				t.Errorf("error = %v, want migration hint %q", err, tc.hint)
			}
			if tc.hint == "" && strings.Contains(err.Error(), "removed") {
				t.Errorf("error = %v, unexpected removal hint for a plain unknown key", err)
			}
		})
	}
}

func TestUncoveredBranchesForExactCoverage(t *testing.T) {
	// setSource on a zero Config initializes the nil map.
	zero := &Config{}
	if err := zero.ApplyFlags(FlagValues{ListenAddr: ":1", WebhookSecret: "s", GitLabURL: "https://flag.example.test", DatabaseURL: "postgres://flag/db"}); err != nil {
		t.Fatal(err)
	}
	if zero.ListenAddr != ":1" || zero.Webhook.Secret != "s" || zero.Dispatch.GitLabURL != "https://flag.example.test" || zero.DatabaseURL != "postgres://flag/db" {
		t.Fatalf("zero-config ApplyFlags = %+v", zero)
	}
	if got := zero.SourceReport(); len(got) != len(multiFormSettingKeys) {
		t.Fatalf("source report lines = %d, want %d", len(got), len(multiFormSettingKeys))
	}

	// Explicit-key decode failure surfaces with the path.
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("listen_addr: ':8080'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldUnmarshal := unmarshalYAML
	unmarshalYAML = func([]byte, interface{}) error { return errors.New("explicit-key boom") }
	t.Cleanup(func() { unmarshalYAML = oldUnmarshal })
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "explicit-key") {
		t.Fatalf("Load explicit-key error = %v", err)
	}
	unmarshalYAML = oldUnmarshal

	// listen_addr fills from env when the file omits it.
	emptyPath := filepath.Join(t.TempDir(), "empty.yaml")
	if err := os.WriteFile(emptyPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(emptyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ApplyEnvironment(LoadEnvironment([]string{"KESTRIX_ADDR=:7070"})); err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != ":7070" {
		t.Fatalf("listen_addr = %q, want env :7070", cfg.ListenAddr)
	}

	// Non-numeric, prefixed, zero, and negative dispatch values fail
	// startup instead of silently enabling dispatch; the error names the
	// variable but never echoes the value.
	for _, raw := range []string{"not-a-number", "222evil", "0", "-5"} {
		cfg2, err := Load(emptyPath)
		if err != nil {
			t.Fatal(err)
		}
		err = cfg2.ApplyEnvironment(LoadEnvironment([]string{"KESTRIX_DISPATCH_PROJECT=" + raw}))
		if err == nil {
			t.Fatalf("KESTRIX_DISPATCH_PROJECT=%q: expected startup error", raw)
		}
		if !strings.Contains(err.Error(), "KESTRIX_DISPATCH_PROJECT") {
			t.Errorf("KESTRIX_DISPATCH_PROJECT=%q: error = %v, want it to name the variable", raw, err)
		}
		if strings.Contains(err.Error(), raw) {
			t.Errorf("KESTRIX_DISPATCH_PROJECT=%q: error = %v, must not echo the value", raw, err)
		}
	}

	// Non-positive history limit normalizes inside ApplyEnvironment.
	bare := &Config{}
	if err := bare.ApplyEnvironment(LoadEnvironment(nil)); err != nil {
		t.Fatal(err)
	}
	if bare.Webhook.HistoryLimit != DefaultWebhookHistoryLimit {
		t.Fatalf("history limit = %d, want default", bare.Webhook.HistoryLimit)
	}
}

func TestSourceReportOmitsValues(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Webhook.Secret = "top-secret-value"
	cfg.Dispatch.GitLabToken = "glpat-top-secret"
	for _, line := range cfg.SourceReport() {
		if strings.Contains(line, "top-secret") || strings.Contains(line, "glpat") {
			t.Fatalf("source report leaks a value: %q", line)
		}
	}
	if len(cfg.SourceReport()) != len(multiFormSettingKeys) {
		t.Fatalf("source report has %d lines, want %d", len(cfg.SourceReport()), len(multiFormSettingKeys))
	}
}

// TestSourceReportLabelsMatchWinningSource proves SourceReport's per-key
// label names the actual highest-precedence winner, not just the first
// non-empty layer scanned - several keys here have two or three layers set
// at once, which is the scenario where a labeling bug would actually show
// up (TestPrecedenceTable already proves the *value* wins correctly, but
// never inspects the label). Covers all four categories (top-level,
// webhook, dispatch, source_cache, runner) plus the never-set default case.
func TestSourceReportLabelsMatchWinningSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	fileYAML := "listen_addr: ':9090'\n" +
		"database_url: postgres://file/db\n" +
		"runner:\n  docker_pids: 100\n"
	if err := os.WriteFile(path, []byte(fileYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	env := LoadEnvironment([]string{
		"KESTRIX_ADDR=:7070",                          // conflicts with file listen_addr
		"DATABASE_URL=postgres://env/db",              // conflicts with file database_url
		"KESTRIX_WEBHOOK_SECRET=env-secret",           // no file form; conflicts with flag below
		"KESTRIX_RUNNER_DOCKER_PIDS=101",              // conflicts with file runner.docker_pids
		"KESTRIX_RUNNER_DOCKER_MEMORY=1g",             // env-only, no file/flag
		"KESTRIX_GITLAB_URL=https://env.example.test", // env-only, no file/flag
	})
	if err := cfg.ApplyEnvironment(env); err != nil {
		t.Fatal(err)
	}
	if err := cfg.ApplyFlags(FlagValues{
		ListenAddr:     ":3000",       // three-way conflict: flag must win over file+env
		WebhookSecret:  "flag-secret", // two-way conflict: flag must win over env
		SourceCacheDir: "/flag/cache", // flag-only, no file/env
	}); err != nil {
		t.Fatal(err)
	}

	labels := make(map[string]string, len(multiFormSettingKeys))
	for _, line := range cfg.SourceReport() {
		key, src, ok := strings.Cut(line, " source=")
		if !ok {
			t.Fatalf("malformed source report line: %q", line)
		}
		labels[key] = src
	}
	want := map[string]string{
		KeyListenAddr:           "flag", // file+env+flag: flag wins
		KeyDatabaseURL:          "file", // file+env: file wins
		KeyWebhookSecret:        "flag", // env+flag: flag wins
		KeyRunnerDockerPIDs:     "file", // file+env: file wins
		KeyRunnerDockerMemory:   "env",  // env only
		KeyDispatchGitLabURL:    "env",  // env only
		KeySourceCacheDirectory: "flag", // flag only
		KeyDefaultBranch:        "default",
	}
	for key, wantSrc := range want {
		if got := labels[key]; got != wantSrc {
			t.Errorf("SourceReport()[%s] = %q, want %q", key, got, wantSrc)
		}
	}
}

// keyConstNamingCollisionHint is appended to failures that could also mean
// "this isn't actually one of the settings-key constants" rather than a real
// multiFormSettingKeys desync - the Key prefix is a naming convention, not a
// language guarantee.
const keyConstNamingCollisionHint = " (if this isn't meant to be a settings-key constant, rename it off the Key prefix)"

// TestKeyConstantsMatchMultiFormSettingKeys parses every non-test source file
// in this package directory and asserts every exported KeyXxx string
// constant, wherever declared, is a member of multiFormSettingKeys and that
// the two collections are the same multiset (not just equal in size).
// Nothing at compile time ties the const block to the slice - a KeyFoo
// constant declared but never added to multiFormSettingKeys would compile
// fine and silently vanish from SourceReport/presence-tracking - so this is
// the AST-based self-check that closes that gap.
//
// This assumes internal/config has no build-tag-gated files (true today:
// config.go, environment.go, task_env.go all build unconditionally). A
// platform-specific file redeclaring a Key* constant under a different
// //go:build tag or GOOS/GOARCH filename suffix would be parsed
// unconditionally here, which this test does not account for.
func TestKeyConstantsMatchMultiFormSettingKeys(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		t.Fatal("found no non-test .go files in this package directory - test setup is broken")
	}

	var keyConsts []string
	for _, file := range files {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.CONST {
				continue
			}
			for _, spec := range genDecl.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range valueSpec.Names {
					if !strings.HasPrefix(name.Name, "Key") {
						continue
					}
					if i >= len(valueSpec.Values) {
						t.Fatalf("%s has no explicit value; this check does not support implicit const repetition%s", name.Name, keyConstNamingCollisionHint)
					}
					lit, ok := valueSpec.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("%s is not a plain string literal constant%s", name.Name, keyConstNamingCollisionHint)
					}
					val, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("unquote %s value %s: %v", name.Name, lit.Value, err)
					}
					keyConsts = append(keyConsts, val)
				}
			}
		}
	}
	if len(keyConsts) == 0 {
		t.Fatal("found no Key* constants in this package - test setup is broken")
	}

	sortedKeyConsts := slices.Sorted(slices.Values(keyConsts))
	sortedSettingKeys := slices.Sorted(slices.Values(multiFormSettingKeys))
	if !slices.Equal(sortedKeyConsts, sortedSettingKeys) {
		t.Errorf("Key* constants and multiFormSettingKeys are not the same set%s\nconstants: %v\nsettings:  %v",
			keyConstNamingCollisionHint, sortedKeyConsts, sortedSettingKeys)
	}
}

// --- observability settings: metrics.enabled, metrics.token, logging.level ---
// See observability design.md and runtime-configuration spec.md
// "Observability settings follow the precedence rule".

func TestObservabilitySettingsDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Metrics.Enabled {
		t.Error("Metrics.Enabled = true, want false default")
	}
	if cfg.Metrics.Token != "" {
		t.Errorf("Metrics.Token = %q, want empty default", cfg.Metrics.Token)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level = %q, want info default", cfg.Logging.Level)
	}
}

func TestObservabilitySettingsFileWinsOverEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("metrics:\n  enabled: true\n  token: file-token\nlogging:\n  level: debug\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	env := LoadEnvironment([]string{
		"KESTRIX_METRICS_ENABLED=false",
		"KESTRIX_METRICS_TOKEN=env-token",
		"KESTRIX_LOG_LEVEL=warn",
	})
	if err := cfg.ApplyEnvironment(env); err != nil {
		t.Fatal(err)
	}
	if !cfg.Metrics.Enabled {
		t.Error("Metrics.Enabled = false, want true (file wins)")
	}
	if cfg.Metrics.Token != "file-token" {
		t.Errorf("Metrics.Token = %q, want file-token (file wins)", cfg.Metrics.Token)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %q, want debug (file wins)", cfg.Logging.Level)
	}
	labels := sourceLabels(t, cfg)
	for _, key := range []string{KeyMetricsEnabled, KeyMetricsToken, KeyLoggingLevel} {
		if labels[key] != "file" {
			t.Errorf("source[%s] = %q, want file", key, labels[key])
		}
	}
}

func TestObservabilitySettingsEnvironmentFillsAbsentKey(t *testing.T) {
	cfg := DefaultConfig()
	env := LoadEnvironment([]string{
		"KESTRIX_METRICS_ENABLED=true",
		"KESTRIX_METRICS_TOKEN=env-token",
		"KESTRIX_LOG_LEVEL=warn",
	})
	if err := cfg.ApplyEnvironment(env); err != nil {
		t.Fatal(err)
	}
	if !cfg.Metrics.Enabled {
		t.Error("Metrics.Enabled = false, want true (env fills absent file key)")
	}
	if cfg.Metrics.Token != "env-token" {
		t.Errorf("Metrics.Token = %q, want env-token (env fills absent file key)", cfg.Metrics.Token)
	}
	if cfg.Logging.Level != "warn" {
		t.Errorf("Logging.Level = %q, want warn (env fills absent file key)", cfg.Logging.Level)
	}
	labels := sourceLabels(t, cfg)
	for _, key := range []string{KeyMetricsEnabled, KeyMetricsToken, KeyLoggingLevel} {
		if labels[key] != "env" {
			t.Errorf("source[%s] = %q, want env", key, labels[key])
		}
	}
}

// sourceLabels indexes cfg.SourceReport() by key, so a test can assert on
// one setting's winning source without parsing the whole report by hand.
func sourceLabels(t *testing.T, cfg *Config) map[string]string {
	t.Helper()
	out := make(map[string]string, len(multiFormSettingKeys))
	for _, line := range cfg.SourceReport() {
		key, src, ok := strings.Cut(line, " source=")
		if !ok {
			t.Fatalf("malformed source report line: %q", line)
		}
		out[key] = src
	}
	return out
}

func TestValidateObservabilitySettingsRejectsUnknownLevel(t *testing.T) {
	for _, level := range []string{"", "verbose", "INFO", "trace"} {
		cfg := DefaultConfig()
		cfg.Logging.Level = level
		err := cfg.validateObservabilitySettings()
		if err == nil {
			t.Errorf("level %q: expected a validation error", level)
			continue
		}
		if !strings.Contains(err.Error(), "logging.level") {
			t.Errorf("level %q: error = %q, want it to name logging.level", level, err.Error())
		}
	}
	for _, level := range []string{"debug", "info", "warn", "error"} {
		cfg := DefaultConfig()
		cfg.Logging.Level = level
		if err := cfg.validateObservabilitySettings(); err != nil {
			t.Errorf("level %q: unexpected error %v", level, err)
		}
	}
}

func TestApplyEnvironmentRejectsUnknownLogLevelFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("logging:\n  level: verbose\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	err = cfg.ApplyEnvironment(LoadEnvironment(nil))
	if err == nil {
		t.Fatal("expected an error for an unrecognized logging.level set in the file")
	}
	if !strings.Contains(err.Error(), "logging.level") {
		t.Errorf("error = %q, want it to name logging.level", err.Error())
	}
}

func TestApplyEnvironmentRejectsInvalidMetricsEnabled(t *testing.T) {
	cfg := DefaultConfig()
	err := cfg.ApplyEnvironment(LoadEnvironment([]string{"KESTRIX_METRICS_ENABLED=maybe"}))
	if err == nil {
		t.Fatal("expected an environment validation error")
	}
	if !strings.Contains(err.Error(), "KESTRIX_METRICS_ENABLED") {
		t.Errorf("error = %q, want it to name KESTRIX_METRICS_ENABLED", err.Error())
	}
}

// TestSourceReportOmitsMetricsToken is the observability-specific
// counterpart to TestSourceReportOmitsValues: metrics.token is a secret
// like webhook.secret and dispatch.gitlab_token, and must never appear in
// the rendered report - see runtime-configuration spec.md "Scrape token
// stays private".
func TestSourceReportOmitsMetricsToken(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Metrics.Token = "super-secret-scrape-token"
	for _, line := range cfg.SourceReport() {
		if strings.Contains(line, "super-secret-scrape-token") {
			t.Fatalf("source report leaks the metrics token: %q", line)
		}
	}
	labels := sourceLabels(t, cfg)
	if _, ok := labels[KeyMetricsToken]; !ok {
		t.Fatal("source report does not name metrics.token at all")
	}
}

// runner.mode is validated: an unknown value fails naming the accepted ones.
func TestRunnerConfigValidate(t *testing.T) {
	for _, mode := range []string{"", "local", "docker", "remote"} {
		if err := (RunnerConfig{Mode: mode}).Validate(); err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
	}
	err := (RunnerConfig{Mode: "bogus"}).Validate()
	if err == nil || !strings.Contains(err.Error(), "runner.mode") || !strings.Contains(err.Error(), "local, docker, remote") {
		t.Fatalf("bogus: err = %v, want it to name runner.mode and the accepted values", err)
	}
}
