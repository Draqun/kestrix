package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type fakeLocalModeCounter struct {
	count int
	err   error
}

func (f fakeLocalModeCounter) CountLocalModeProjects() (int, error) {
	return f.count, f.err
}

func TestWarnLocalModeProjects_NoOpWhenNotAllowed(t *testing.T) {
	var out bytes.Buffer
	if err := warnLocalModeProjects(fakeLocalModeCounter{count: 5}, false, &out); err != nil {
		t.Fatalf("warnLocalModeProjects: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("log output = %q, want nothing logged when the opt-in is off", out.String())
	}
}

func TestWarnLocalModeProjects_LogsCountWhenAllowed(t *testing.T) {
	var out bytes.Buffer
	if err := warnLocalModeProjects(fakeLocalModeCounter{count: 3}, true, &out); err != nil {
		t.Fatalf("warnLocalModeProjects: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "3 stored project(s)") {
		t.Fatalf("log output = %q, want it to report the count", got)
	}
	if !strings.Contains(got, "KESTRIX_RUNNER_ALLOW_LOCAL") {
		t.Fatalf("log output = %q, want it to name the opt-in", got)
	}
}

func TestWarnLocalModeProjects_LogsZeroWhenAllowed(t *testing.T) {
	var out bytes.Buffer
	if err := warnLocalModeProjects(fakeLocalModeCounter{count: 0}, true, &out); err != nil {
		t.Fatalf("warnLocalModeProjects: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "0 stored project(s)") {
		t.Fatalf("log output = %q, want it to report zero", got)
	}
}

func TestWarnLocalModeProjects_StoreErrorNamesTheStep(t *testing.T) {
	var out bytes.Buffer
	err := warnLocalModeProjects(fakeLocalModeCounter{err: errors.New("connection refused")}, true, &out)
	if err == nil {
		t.Fatal("expected an error when the store fails")
	}
	if !strings.Contains(err.Error(), "count local-mode projects") {
		t.Fatalf("error = %q, want it to name the step", err.Error())
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("error = %q, want the underlying store error", err.Error())
	}
	if out.Len() != 0 {
		t.Fatalf("log output = %q, want nothing logged on failure", out.String())
	}
}

// TestCLI_Serve_RefusesLocalModeWithoutOptIn drives the real `kestrix serve`
// binary, not just the RunnerConfig.Validate() unit test: a fresh config (no
// file, no KESTRIX_RUNNER_ALLOW_LOCAL) resolves to instance mode `local`,
// and cfg.Runner.Validate() in cmdServe must fail the process before it ever
// reaches NewServerE/ListenAndServe - see safe-production-execution
// spec.md "Local instance mode without the opt-in". A clean env (no
// os.Environ() passthrough) rules out an ambient KESTRIX_* variable
// masking a regression that moved or dropped the Validate() call.
func TestCLI_Serve_RefusesLocalModeWithoutOptIn(t *testing.T) {
	bin := buildKestrix(t)
	cmd := exec.Command(bin, "serve",
		"--config", filepath.Join(t.TempDir(), "missing-config.yaml"),
		"--source-cache-dir", t.TempDir(),
	)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected the process to exit with an error, got %v (stderr=%s)", err, stderr.String())
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr=%s)", exitErr.ExitCode(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "KESTRIX_RUNNER_ALLOW_LOCAL") {
		t.Fatalf("stderr = %q, want it to name KESTRIX_RUNNER_ALLOW_LOCAL", stderr.String())
	}
}
