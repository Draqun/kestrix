// Black-box CLI tests for cmd/kestrix via os/exec. The main package
// calls os.Exit on all paths, so we drive it as a subprocess and
// assert on exit code + stdout/stderr.
package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildKestrix compiles the kestrix binary into t.TempDir()/kestrix.
func buildKestrix(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "kestrix")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build -o %s: %v\n%s", bin, err, out)
	}
	return bin
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
