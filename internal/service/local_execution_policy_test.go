package service

import (
	"context"
	"errors"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
)

// TestRunService_CheckLocalAllowed pins safe-production-execution
// spec.md "Stored local project does not execute": a run resolving to
// `local` mode is refused unless SetLocalExecutionPolicy(true, ...) opted
// in, and any other resolved mode is always allowed. An empty project mode
// resolves against the instance default the same way modeSelectingExecutor
// does.
func TestRunService_CheckLocalAllowed(t *testing.T) {
	cases := []struct {
		name        string
		allowLocal  bool
		defaultMode string
		mode        string
		wantErr     bool
	}{
		{"explicit local without opt-in", false, "remote", "local", true},
		{"empty mode resolves to local default without opt-in", false, "local", "", true},
		{"empty mode resolves to local default (default unset) without opt-in", false, "", "", true},
		{"explicit local with opt-in", true, "remote", "local", false},
		{"empty mode resolves to local default with opt-in", true, "local", "", false},
		{"docker mode without opt-in", false, "local", "docker", false},
		{"remote mode without opt-in", false, "local", "remote", false},
		{"empty mode resolves to non-local default without opt-in", false, "remote", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewRunService(nil, nil, nil)
			s.SetLocalExecutionPolicy(tc.allowLocal, tc.defaultMode)
			err := s.CheckLocalAllowed(tc.mode)
			if tc.wantErr && err == nil {
				t.Fatal("expected an error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRunService_ServiceLevelLocalRunnerGuard(t *testing.T) {
	proj := &domain.Project{
		Name:  "test-proj",
		Tasks: []domain.Task{{Name: "test-task"}},
	}

	t.Run("Start returns ErrLocalExecutionDisabled without fetching sources", func(t *testing.T) {
		mockStore := &mocks.MockRunStore{}
		pipelineRunner := &mocks.MockPipelineRunner{}
		s := NewRunService(mockStore, pipelineRunner, pipelineRunner)
		s.SetLocalExecutionPolicy(false, "local")

		sourcesAcquired := false
		opts := domain.RunnerOptions{
			Mode: "local",
			OnSourcesAcquired: func([]domain.AcquiredSource) error {
				sourcesAcquired = true
				return nil
			},
		}

		runID, err := s.Start(context.Background(), "test-proj", proj, nil, opts)
		if !errors.Is(err, ErrLocalExecutionDisabled) {
			t.Fatalf("expected ErrLocalExecutionDisabled, got: %v", err)
		}
		if runID != 0 {
			t.Fatalf("expected runID 0, got: %d", runID)
		}
		if len(mockStore.CreateRunCalls) != 0 {
			t.Fatalf("expected no CreateRun calls, got: %d", len(mockStore.CreateRunCalls))
		}
		if sourcesAcquired {
			t.Fatal("expected no sources to be acquired")
		}
		if len(pipelineRunner.RunCalls) != 0 {
			t.Fatalf("expected no pipeline runner calls, got: %d", len(pipelineRunner.RunCalls))
		}
	})

	t.Run("StartTask returns ErrLocalExecutionDisabled without fetching sources", func(t *testing.T) {
		mockStore := &mocks.MockRunStore{}
		taskRunner := &mocks.MockPipelineRunner{}
		s := NewRunService(mockStore, taskRunner, taskRunner)
		s.SetLocalExecutionPolicy(false, "local")

		sourcesAcquired := false
		opts := domain.RunnerOptions{
			Mode: "local",
			OnSourcesAcquired: func([]domain.AcquiredSource) error {
				sourcesAcquired = true
				return nil
			},
		}

		_, queued, err := s.StartTask(context.Background(), "test-proj", proj, "test-task", opts)
		if !errors.Is(err, ErrLocalExecutionDisabled) {
			t.Fatalf("expected ErrLocalExecutionDisabled, got: %v", err)
		}
		if queued {
			t.Fatal("expected queued to be false")
		}
		if sourcesAcquired {
			t.Fatal("expected no sources to be acquired")
		}
		if len(taskRunner.TaskRunCalls) != 0 {
			t.Fatalf("expected no task runner calls, got: %d", len(taskRunner.TaskRunCalls))
		}
	})

	t.Run("ApproveManualTask returns ErrLocalExecutionDisabled without fetching sources", func(t *testing.T) {
		mockStore := &mocks.MockRunStore{}
		taskRunner := &mocks.MockPipelineRunner{}
		s := NewRunService(mockStore, taskRunner, taskRunner)
		s.SetLocalExecutionPolicy(false, "local")

		sourcesAcquired := false
		opts := domain.RunnerOptions{
			Mode: "local",
			OnSourcesAcquired: func([]domain.AcquiredSource) error {
				sourcesAcquired = true
				return nil
			},
		}

		tr, err := s.ApproveManualTask(context.Background(), 1, "test-proj", proj, "test-task", opts)
		if !errors.Is(err, ErrLocalExecutionDisabled) {
			t.Fatalf("expected ErrLocalExecutionDisabled, got: %v", err)
		}
		if tr.Status != "" {
			t.Fatalf("expected empty task result, got: %+v", tr)
		}
		if sourcesAcquired {
			t.Fatal("expected no sources to be acquired")
		}
		if len(taskRunner.TaskRunCalls) != 0 {
			t.Fatalf("expected no task runner calls, got: %d", len(taskRunner.TaskRunCalls))
		}
	})

	t.Run("RunTask returns ErrLocalExecutionDisabled without fetching sources", func(t *testing.T) {
		mockStore := &mocks.MockRunStore{}
		taskRunner := &mocks.MockPipelineRunner{}
		s := NewRunService(mockStore, taskRunner, taskRunner)
		s.SetLocalExecutionPolicy(false, "local")

		sourcesAcquired := false
		opts := domain.RunnerOptions{
			Mode: "local",
			OnSourcesAcquired: func([]domain.AcquiredSource) error {
				sourcesAcquired = true
				return nil
			},
		}

		tr, dispatched, err := s.RunTask(context.Background(), 1, "test-proj", proj, "test-task", opts)
		if !errors.Is(err, ErrLocalExecutionDisabled) {
			t.Fatalf("expected ErrLocalExecutionDisabled, got: %v", err)
		}
		if dispatched {
			t.Fatal("expected dispatched to be false")
		}
		if tr.Status != "" {
			t.Fatalf("expected empty task result, got: %+v", tr)
		}
		if sourcesAcquired {
			t.Fatal("expected no sources to be acquired")
		}
		if len(taskRunner.TaskRunCalls) != 0 {
			t.Fatalf("expected no task runner calls, got: %d", len(taskRunner.TaskRunCalls))
		}
	})
}
