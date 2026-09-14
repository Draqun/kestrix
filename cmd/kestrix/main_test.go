// Black-box CLI tests for cmd/kestrix via os/exec. The main package
// calls os.Exit on all paths, so we drive it as a subprocess and
// assert on exit code + stdout/stderr.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"kestrix/internal/config"
)

var (
	kestrixBinOnce     sync.Once
	kestrixBinDir      string
	kestrixBinPath     string
	kestrixBinErr      error
	kestrixBinReported atomic.Bool
)

// TestMain removes the binary the tests share once they are all done.
func TestMain(m *testing.M) {
	code := m.Run()
	if kestrixBinDir != "" {
		_ = os.RemoveAll(kestrixBinDir)
	}
	os.Exit(code)
}

// buildKestrix compiles the kestrix binary once per package run and returns
// the path every test shares. Building it per test cost a linker pass each
// time - minutes in CI across the twenty-odd tests here, enough to exceed
// go test's package timeout - and they all drive the same binary.
func buildKestrix(t *testing.T) string {
	t.Helper()
	kestrixBinOnce.Do(func() {
		kestrixBinDir, kestrixBinErr = os.MkdirTemp("", "kestrix-cli-bin-")
		if kestrixBinErr != nil {
			return
		}
		kestrixBinPath = filepath.Join(kestrixBinDir, "kestrix")
		cmd := exec.Command("go", "build", "-o", kestrixBinPath, ".")
		cmd.Dir = "."
		if out, err := cmd.CombinedOutput(); err != nil {
			kestrixBinErr = fmt.Errorf("go build -o %s: %v\n%s", kestrixBinPath, err, out)
		}
	})
	if kestrixBinErr != nil {
		// Every test here needs the same binary, so a broken build would
		// otherwise repeat one message twenty times and bury the first.
		if kestrixBinReported.Swap(true) {
			t.Skip("kestrix binary could not be built; see the first failure in this package")
		}
		t.Fatalf("build kestrix: %v", kestrixBinErr)
	}
	return kestrixBinPath
}

func runKestrix(t *testing.T, bin string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run %s: %v", bin, err)
	}
	return so.String(), se.String(), code
}

func writePipeline(t *testing.T) string {
	t.Helper()
	pipeline := `project "cli-test" {
		ci_repo "svc" {
			path = "."
			kind = "subpath"
		}
		ci_task "build" {
			trigger = "repo.svc.changed"
			execute {
				repo = "svc"
				script = ":"
			}
		}
	}`
	d := t.TempDir()
	path := filepath.Join(d, "pipeline.tf")
	if err := os.WriteFile(path, []byte(pipeline), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCLI_NoArgs(t *testing.T) {
	bin := buildKestrix(t)
	_, stderr, code := runKestrix(t, bin)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "Usage:") && !strings.Contains(stderr, "kestrix") {
		t.Errorf("stderr should include usage, got %q", stderr)
	}
}

func TestCLI_Help(t *testing.T) {
	bin := buildKestrix(t)
	_, stderr, code := runKestrix(t, bin, "help")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stderr, "kestrix") {
		t.Errorf("help should mention kestrix, got %q", stderr)
	}
}

func TestCLI_UnknownCommand(t *testing.T) {
	bin := buildKestrix(t)
	_, stderr, code := runKestrix(t, bin, "frobnicate")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Errorf("expected 'unknown command' message, got %q", stderr)
	}
}

func TestCLI_Validate_Success(t *testing.T) {
	bin := buildKestrix(t)
	pipeline := writePipeline(t)
	stdout, stderr, code := runKestrix(t, bin, "validate", pipeline)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "validated") {
		t.Errorf("expected 'validated', got: %q", stdout)
	}
}

func TestCLI_Validate_MissingFile(t *testing.T) {
	bin := buildKestrix(t)
	_, stderr, code := runKestrix(t, bin, "validate", "/no/such/file.tf")
	if code == 0 {
		t.Fatal("expected non-zero exit for missing file")
	}
	if !strings.Contains(stderr, "parse error") {
		t.Errorf("expected 'parse error', got %q", stderr)
	}
}

func TestCLI_Validate_NoArgs(t *testing.T) {
	bin := buildKestrix(t)
	_, stderr, code := runKestrix(t, bin, "validate")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "pipeline file required") {
		t.Errorf("expected 'pipeline file required', got %q", stderr)
	}
}

func TestCLI_Resolve(t *testing.T) {
	bin := buildKestrix(t)
	pipeline := writePipeline(t)
	stdout, _, code := runKestrix(t, bin, "resolve", "--changed", "svc", pipeline)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{"Project:", "Tasks triggered", "build"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in output: %s", want, stdout)
		}
	}
}

func TestCLI_Resolve_WarningForUnknown(t *testing.T) {
	bin := buildKestrix(t)
	pipeline := writePipeline(t)
	stdout, stderr, code := runKestrix(t, bin, "resolve", "--changed", "svc,ghost", pipeline)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (warning shouldn't fail)", code)
	}
	if !strings.Contains(stderr, "ghost") {
		t.Errorf("expected warning about ghost, got: stderr=%q stdout=%q", stderr, stdout)
	}
}

func TestCLI_Resolve_NoArgs(t *testing.T) {
	bin := buildKestrix(t)
	_, stderr, code := runKestrix(t, bin, "resolve")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "pipeline file required") {
		t.Errorf("expected 'pipeline file required', got %q", stderr)
	}
}

func TestCLI_Variables(t *testing.T) {
	bin := buildKestrix(t)
	pipeline := `project "vars-test" {
		variable "FOO" {
			value = "bar"
			scope = "project"
		}
		variable "BAZ" {
			value     = "qux"
			scope     = "project"
			masked    = true
			protected = true
		}
	}`
	d := t.TempDir()
	path := filepath.Join(d, "pipeline.tf")
	if err := os.WriteFile(path, []byte(pipeline), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := runKestrix(t, bin, "variables", path)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{"FOO", "bar", "BAZ", "qux", "Resolved variables"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in output: %s", want, stdout)
		}
	}
}

func TestCLI_Variables_NoArgs(t *testing.T) {
	bin := buildKestrix(t)
	_, _, code := runKestrix(t, bin, "variables")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestCLI_Run_NoArgs(t *testing.T) {
	bin := buildKestrix(t)
	_, stderr, code := runKestrix(t, bin, "run")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "pipeline file required") {
		t.Errorf("expected 'pipeline file required', got %q", stderr)
	}
}

func TestCLI_Run_BadFile(t *testing.T) {
	bin := buildKestrix(t)
	_, stderr, code := runKestrix(t, bin, "run", "/no/such/pipeline.tf")
	if code == 0 {
		t.Fatal("expected non-zero exit")
	}
	if !strings.Contains(stderr, "parse error") {
		t.Errorf("expected 'parse error', got %q", stderr)
	}
}

func TestCLI_ChangedSummary_Empty(t *testing.T) {
	// changedSummary is unexported; tested via cmdResolve.
	bin := buildKestrix(t)
	pipeline := writePipeline(t)
	stdout, _, code := runKestrix(t, bin, "resolve", "--changed", "", pipeline)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "(none)") {
		t.Errorf("empty changed-set should print '(none)', got: %s", stdout)
	}
}

func TestDefaultLocalDatabaseURLIsIsolated(t *testing.T) {
	const expected = "postgres://kestrix:kestrix@localhost:5433/kestrix_local?sslmode=disable"
	if defaultLocalDatabaseURL != expected {
		t.Fatalf("default local database URL = %q, want %q", defaultLocalDatabaseURL, expected)
	}
}

func TestLogConfigSourcesMissingFile(t *testing.T) {
	cfg := config.DefaultConfig()
	var out bytes.Buffer
	logConfigSources(&out, cfg, "/no/such/config.yaml")
	got := out.String()
	if !strings.Contains(got, "config: /no/such/config.yaml") {
		t.Errorf("output missing config path line: %q", got)
	}
	if !strings.Contains(got, "no config file found") {
		t.Errorf("output missing missing-file branch: %q", got)
	}
	for _, key := range []string{"listen_addr", "dispatch.project_id", "runner.cancel_grace"} {
		if !strings.Contains(got, "config: "+key+" source=default") {
			t.Errorf("output missing source line for %s: %q", key, got)
		}
	}
}

// TestLogConfigSourcesOmitsSecrets proves the per-line Fprintf loop, not just
// Config.SourceReport(), never writes a configured secret or token to the log.
func TestLogConfigSourcesOmitsSecrets(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Webhook.Secret = "canary-webhook-secret-9z"
	cfg.Dispatch.GitLabToken = "canary-gitlab-token-9z"

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	logConfigSources(&out, cfg, path)
	got := out.String()
	if strings.Contains(got, "canary-webhook-secret-9z") || strings.Contains(got, "canary-gitlab-token-9z") {
		t.Fatalf("logConfigSources leaked a secret: %q", got)
	}
	if strings.Contains(got, "no config file found") {
		t.Errorf("existing config file wrongly reported as missing: %q", got)
	}
}
