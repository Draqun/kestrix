package config

// Unit coverage for the runner-docker-config-wiring change: WithRunnerConfig
// derives a snapshot carrying the reconciled runner settings without
// mutating the receiver, and ReplaceCurrent swaps the process snapshot once
// at startup while returning the previous snapshot for test restoration.
// These tests live in internal/config (not internal/runner) because the
// unit coverage gate excludes /internal/runner/ when Docker is unavailable.

import (
	"testing"
	"time"
)

func TestWithRunnerConfigCopiesReconciledSettings(t *testing.T) {
	base := &Environment{RunnerDockerMemory: "env-mem", RunnerCancelGrace: time.Second}
	cfg := DefaultConfig()
	cfg.Runner.DockerPIDs = 128
	cfg.Runner.DockerMemory = "3g"
	cfg.Runner.DockerCPUs = "1.5"
	cfg.Runner.DockerUser = "1000:1000"
	cfg.Runner.DockerNetwork = "none"
	cfg.Runner.CancelGrace = Duration(45 * time.Second)

	derived := base.WithRunnerConfig(cfg)

	if derived.RunnerDockerPIDs != 128 ||
		derived.RunnerDockerMemory != "3g" ||
		derived.RunnerDockerCPUs != "1.5" ||
		derived.RunnerDockerUser != "1000:1000" ||
		derived.RunnerDockerNetwork != "none" ||
		derived.RunnerCancelGrace != 45*time.Second {
		t.Fatalf("derived snapshot = %+v, want reconciled Config.Runner values", derived)
	}
	if base.RunnerDockerMemory != "env-mem" || base.RunnerCancelGrace != time.Second {
		t.Fatalf("receiver mutated: %+v", base)
	}
}

func TestReplaceCurrentSwapsAndReturnsPrevious(t *testing.T) {
	previous := Current()
	if previous == nil {
		t.Fatal("Current() returned nil")
	}
	replacement := &Environment{RunnerDockerMemory: "replacement"}
	t.Cleanup(func() { ReplaceCurrent(previous) })

	if got := ReplaceCurrent(replacement); got != previous {
		t.Fatalf("ReplaceCurrent returned %p, want previous %p", got, previous)
	}
	if Current() != replacement {
		t.Fatal("Current() did not return the replacement snapshot")
	}
}
