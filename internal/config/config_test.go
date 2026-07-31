package config

import (
	"errors"
	"os"
	"path/filepath"
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
	if err := cfg.ApplySourceCacheEnvironment(env); err != nil {
		t.Fatal(err)
	}
	if cfg.SourceCache.Directory != fileCache || time.Duration(cfg.SourceCache.MaxAge) != 48*time.Hour || cfg.SourceCache.MaxBytes != 2048 {
		t.Fatalf("file did not override environment: %+v", cfg.SourceCache)
	}
	cliCache := filepath.Join(dir, "cli-cache")
	cfg.ApplySourceCacheFlags(cliCache, 2*time.Hour, 4096)
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

func TestLoadReportsPresenceDecodeFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("listen_addr: ':8080'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := unmarshalYAML
	calls := 0
	unmarshalYAML = func(data []byte, target interface{}) error {
		calls++
		if calls == 2 {
			return errors.New("presence decode failed")
		}
		return yaml.Unmarshal(data, target)
	}
	t.Cleanup(func() { unmarshalYAML = old })
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "presence") {
		t.Fatalf("Load error = %v", err)
	}
}

func TestApplySourceCacheEnvironmentBranches(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.ApplySourceCacheEnvironment(nil); err != nil {
		t.Fatal(err)
	}
	invalid := LoadEnvironment([]string{"KESTRIX_SOURCE_CACHE_MAX_AGE=invalid"})
	if err := cfg.ApplySourceCacheEnvironment(invalid); err == nil {
		t.Fatal("expected environment validation error")
	}
	env := LoadEnvironment([]string{
		"KESTRIX_SOURCE_CACHE_DIR=/env/cache",
		"KESTRIX_SOURCE_CACHE_MAX_AGE=12h",
		"KESTRIX_SOURCE_CACHE_MAX_BYTES=1234",
	})
	if err := cfg.ApplySourceCacheEnvironment(env); err != nil {
		t.Fatal(err)
	}
	if cfg.SourceCache.Directory != "/env/cache" || time.Duration(cfg.SourceCache.MaxAge) != 12*time.Hour || cfg.SourceCache.MaxBytes != 1234 {
		t.Fatalf("source cache = %+v", cfg.SourceCache)
	}
}

func TestApplyFlags(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ApplyFlags(":3000", "/opt/pipelines", "release", "glpat-yep", true, 99999)

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
	cfg.ApplyFlags("", "", "", "", false, 0)

	if cfg.ListenAddr != ":8080" {
		t.Errorf("empty ApplyFlags changed ListenAddr to %q", cfg.ListenAddr)
	}
	if cfg.Branch != "main" {
		t.Errorf("empty ApplyFlags changed Branch to %q", cfg.Branch)
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
	_, err := Load("/proc")
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
