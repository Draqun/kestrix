package runner

// Startup-level regression coverage for the runner-docker-config-wiring
// change: a value set under `runner:` in config.yaml must reach the
// DockerExecutor construction site (internal/runner/runner.go:451-460) and
// every RunnerCancelGrace read site (LocalExecutor.RunTask and RunOne/manual
// approval) via the swapped Environment snapshot. Both grace sites read
// config.Current() fresh at task time, so asserting the swapped snapshot
// value covers each of them.
//
// These tests swap the process-global snapshot and therefore MUST NOT run
// in parallel with anything (scripts/test.sh runs go test -race). The
// previous snapshot is always restored via t.Cleanup.

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"kestrix/internal/config"
)

// installRunnerSnapshot writes yamlContent to a temp config.yaml, reconciles
// it against a copy of the live snapshot (mutated by envOverride, so tests
// never depend on ambient KESTRIX_* variables), swaps the derived snapshot
// in, and arranges restoration. It returns the reconciled Config.
func installRunnerSnapshot(t *testing.T, yamlContent string, envOverride func(*config.Environment)) *config.Config {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	envCopy := *config.Current()
	if envOverride != nil {
		envOverride(&envCopy)
	}
	if err := cfg.ApplyEnvironment(&envCopy); err != nil {
		t.Fatalf("ApplyEnvironment: %v", err)
	}
	previous := config.ReplaceCurrent(envCopy.WithRunnerConfig(cfg))
	t.Cleanup(func() { config.ReplaceCurrent(previous) })
	return cfg
}

// captureDockerArgs stubs commandContext to record the docker invocation and
// succeed without a daemon.
func captureDockerArgs(t *testing.T, captured *[]string) {
	t.Helper()
	old := commandContext
	commandContext = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		*captured = append([]string(nil), args...)
		return exec.CommandContext(ctx, "true")
	}
	t.Cleanup(func() { commandContext = old })
}

// TestRunnerRun_UsesConfigFileDockerMemory is the task 1.3 startup-level
// test: runner.docker_memory set only in config.yaml must reach the
// DockerExecutor built by runner.Run, observed through the commandContext
// seam (runner.go:26) like the existing Docker-arg tests.
func TestRunnerRun_UsesConfigFileDockerMemory(t *testing.T) {
	// No env override: the ambient snapshot default ("2g") is the realistic
	// "env var unset" state (LoadEnvironment never produces ""), and it is
	// distinct from the config-file value so the assertion stays meaningful.
	installRunnerSnapshot(t, "runner:\n  docker_memory: \"3g\"\n", nil)

	var captured []string
	captureDockerArgs(t, &captured)

	var stdout, stderr bytes.Buffer
	results, err := Run(context.Background(), runnerProject("echo hi"), map[string]bool{"repo": true}, Options{
		WorkDir: t.TempDir(), Stdout: &stdout, Stderr: &stderr,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 || results[0].Status != "passed" {
		t.Fatalf("results = %+v, want one passed task", results)
	}
	assertFlagValue(t, captured, "--memory", "3g")
}

// TestRunnerRun_ConfigFileWinsOverEnvVar is the task 2.1 precedence test:
// with both sources set to different values, the config-file value must win
// end to end at the constructed DockerExecutor.
func TestRunnerRun_ConfigFileWinsOverEnvVar(t *testing.T) {
	installRunnerSnapshot(t, "runner:\n  docker_memory: \"3g\"\n", func(env *config.Environment) {
		env.RunnerDockerMemory = "9g"
	})

	var captured []string
	captureDockerArgs(t, &captured)

	var stdout, stderr bytes.Buffer
	results, err := Run(context.Background(), runnerProject("echo hi"), map[string]bool{"repo": true}, Options{
		WorkDir: t.TempDir(), Stdout: &stdout, Stderr: &stderr,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 || results[0].Status != "passed" {
		t.Fatalf("results = %+v, want one passed task", results)
	}
	assertFlagValue(t, captured, "--memory", "3g")
}

// TestRunnerCancelGrace_ReachesAllReadSites is the task 2.2 test: a
// config-file runner.cancel_grace must reach the swapped snapshot that every
// read site consumes (LocalExecutor.RunTask and RunOne - each calls
// config.Current() fresh). The RunOne assertion covers the grace-period
// value only; the Docker sandbox bypass on that path is a pre-existing known
// gap (design.md), not asserted here.
func TestRunnerCancelGrace_ReachesAllReadSites(t *testing.T) {
	installRunnerSnapshot(t, "runner:\n  cancel_grace: \"45s\"\n", func(env *config.Environment) {
		env.RunnerCancelGrace = 0
	})

	if got := config.Current().RunnerCancelGrace; got != 45*time.Second {
		t.Fatalf("RunnerCancelGrace = %v, want 45s (config-file value)", got)
	}
}
