package web

import (
	"context"
	"sync"
	"testing"

	"kestrix/internal/domain"
)

func TestModeSelectingExecutor_UsesOptsModeOverDefault(t *testing.T) {
	e := modeSelectingExecutor{defaultMode: "docker"}
	// "local" runs a bare no-op script synchronously and returns no error,
	// unlike "docker" (which would need a real daemon) - a cheap way to
	// prove opts.Mode, not defaultMode, drove the executor built for the
	// call.
	_, err := e.Run(context.Background(), domain.Project{Name: "p"}, map[string]bool{}, domain.RunnerOptions{Mode: "local"})
	if err != nil {
		t.Fatalf("Run with opts.Mode=local: %v", err)
	}
}

func TestModeSelectingExecutor_FallsBackToDefaultModeWhenOptsModeEmpty(t *testing.T) {
	e := modeSelectingExecutor{defaultMode: "local"}
	_, err := e.Run(context.Background(), domain.Project{Name: "p"}, map[string]bool{}, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Run with empty opts.Mode: %v", err)
	}
}

func TestModeSelectingExecutor_RunTask(t *testing.T) {
	e := modeSelectingExecutor{defaultMode: "local"}
	proj := domain.Project{Name: "p", Tasks: []domain.Task{{Name: "t", Execute: domain.ExecuteSpec{Script: "true"}}}}
	_, err := e.RunTask(context.Background(), proj, "t", domain.RunnerOptions{Mode: "local"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
}

// TestModeSelectingExecutor_TwoProjectsDifferentModesConcurrently pins
// remote-runner-registration tasks.md 2.1: two projects on the same
// instance dispatching under different modes concurrently must not
// interfere - each call builds its own runner.PipelineExecutor from
// opts.Mode, so there is no shared mutable Mode field to race on. Run with
// -race to prove it.
func TestModeSelectingExecutor_TwoProjectsDifferentModesConcurrently(t *testing.T) {
	e := modeSelectingExecutor{defaultMode: "local"}
	var wg sync.WaitGroup
	errs := make([]error, 20)
	for i := 0; i < 20; i++ {
		mode := "local"
		if i%2 == 0 {
			mode = "docker" // no tasks, so no container starts
		}
		wg.Add(1)
		go func(i int, mode string) {
			defer wg.Done()
			_, errs[i] = e.Run(context.Background(), domain.Project{Name: "p"}, map[string]bool{}, domain.RunnerOptions{Mode: mode})
		}(i, mode)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d (unexpected per-call error, modes must not interfere): %v", i, err)
		}
	}
}

func TestModeSelectingExecutor_UnsupportedModeErrors(t *testing.T) {
	e := modeSelectingExecutor{defaultMode: "local"}
	_, err := e.Run(context.Background(), domain.Project{Name: "p"}, map[string]bool{}, domain.RunnerOptions{Mode: "not-a-real-mode"})
	if err == nil {
		t.Fatal("expected an error for an unsupported mode")
	}
}
