package service

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/model"
)

// ui-single-task-rerun design.md D1/D2: StartTask accepts a single-task run
// synchronously and executes it in the background through executeStartedTask.

func startTaskProject() *model.Project {
	return &model.Project{Name: "p", Tasks: []model.Task{{Name: "t"}}}
}

func waitFinish(t *testing.T, ch chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for FinishRun")
	}
}

func TestRunService_StartTask_RejectsNilProject(t *testing.T) {
	svc := NewRunService(&mocks.MockRunStore{}, nil, &emptyTaskRunner{})
	if _, err := svc.StartTask(context.Background(), 1, "p", nil, "t", domain.RunnerOptions{}); err == nil {
		t.Fatal("expected error for nil project")
	}
}

func TestRunService_StartTask_RejectsNilTaskRunner(t *testing.T) {
	svc := NewRunService(&mocks.MockRunStore{}, nil, nil)
	if _, err := svc.StartTask(context.Background(), 1, "p", startTaskProject(), "t", domain.RunnerOptions{}); err == nil {
		t.Fatal("expected error for nil task runner")
	}
}

func TestRunService_StartTask_ReturnsBeforeTaskExecutes(t *testing.T) {
	store := &mocks.MockRunStore{FinishRunCh: make(chan struct{})}
	runner := &blockingTaskRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{}), returnedCh: make(chan struct{})}
	svc := NewRunService(store, nil, runner)

	queued, err := svc.StartTask(context.Background(), 5, "p", startTaskProject(), "t", domain.RunnerOptions{SourcesPrepared: true})
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if queued {
		t.Fatal("queued = true, want false with a free slot")
	}
	select {
	case <-runner.returnedCh:
		t.Fatal("task finished before StartTask returned")
	default:
	}
	waitActivePresent(t, svc, 5)

	close(runner.releaseCh)
	waitFinish(t, store.FinishRunCh)
	finish := store.FinishRunCalls[0]
	if finish.ID != 5 || finish.Status != "passed" {
		t.Fatalf("FinishRun = %+v, want run 5 passed", finish)
	}
}

func TestRunService_StartTask_QueuedWhenEverySlotIsBusy(t *testing.T) {
	store := &mocks.MockRunStore{StartQueuedRunResult: true, FinishRunCh: make(chan struct{})}
	runner := &blockingTaskRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{}), returnedCh: make(chan struct{})}
	svc := NewRunService(store, nil, runner)
	svc.SetMaxConcurrentRuns(1)
	if !svc.tryAcquireSlot() {
		t.Fatal("expected the single slot to be free")
	}

	queued, err := svc.StartTask(context.Background(), 6, "p", startTaskProject(), "t", domain.RunnerOptions{SourcesPrepared: true})
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if !queued {
		t.Fatal("queued = false, want true with every slot busy")
	}
	if store.SetRunQueuedCallCount() != 1 {
		t.Fatalf("SetRunQueued calls = %d, want 1 recorded before StartTask returned", store.SetRunQueuedCallCount())
	}
	select {
	case <-runner.startedCh:
		t.Fatal("task started despite the concurrency limit")
	case <-time.After(50 * time.Millisecond):
	}

	svc.releaseSlot()
	select {
	case <-runner.startedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for the queued task to start")
	}
	close(runner.releaseCh)
	waitFinish(t, store.FinishRunCh)
}

func TestRunService_ExecuteStartedTask_Passed(t *testing.T) {
	store := &mocks.MockRunStore{}
	runner := &mocks.MockPipelineRunner{TaskResultList: []domain.TaskResult{{Name: "t", Status: "passed", Output: "ok"}}}
	svc := NewRunService(store, nil, runner)
	untracked := false

	svc.executeStartedTask(context.Background(), func() { untracked = true }, true, 3, "p", startTaskProject(), "t", domain.RunnerOptions{SourcesPrepared: true})

	if !untracked {
		t.Error("untrack was not called")
	}
	if len(store.FinishRunCalls) != 1 {
		t.Fatalf("FinishRunCalls = %+v, want 1", store.FinishRunCalls)
	}
	finish := store.FinishRunCalls[0]
	if finish.Status != "passed" || finish.Output != "ok" || finish.ErrMsg != "" {
		t.Fatalf("FinishRun = %+v, want passed with output ok", finish)
	}
	if len(finish.TaskResults) != 1 || finish.TaskResults[0].Name != "t" {
		t.Fatalf("task results = %+v, want the one task", finish.TaskResults)
	}
}

func TestRunService_ExecuteStartedTask_Failed(t *testing.T) {
	store := &mocks.MockRunStore{}
	runner := &mocks.MockPipelineRunner{TaskResultList: []domain.TaskResult{{Name: "t", Status: "failed", Error: "exit 1"}}}
	svc := NewRunService(store, nil, runner)

	svc.executeStartedTask(context.Background(), func() {}, true, 3, "p", startTaskProject(), "t", domain.RunnerOptions{SourcesPrepared: true})

	if len(store.FinishRunCalls) != 1 {
		t.Fatalf("FinishRunCalls = %+v, want 1", store.FinishRunCalls)
	}
	if finish := store.FinishRunCalls[0]; finish.Status != "failed" || finish.ErrMsg != "exit 1" {
		t.Fatalf("FinishRun = %+v, want failed with exit 1", finish)
	}
}

// Risks: a failure inside the background half must still leave the run
// terminal, even though no HTTP handler is left to observe it.
func TestRunService_ExecuteStartedTask_TaskRunnerErrorLeavesRunTerminal(t *testing.T) {
	store := &mocks.MockRunStore{}
	svc := NewRunService(store, nil, &emptyTaskRunner{})

	svc.executeStartedTask(context.Background(), func() {}, true, 4, "p", startTaskProject(), "t", domain.RunnerOptions{SourcesPrepared: true})

	if len(store.FinishRunCalls) != 1 {
		t.Fatalf("FinishRunCalls = %+v, want 1", store.FinishRunCalls)
	}
	finish := store.FinishRunCalls[0]
	if finish.ID != 4 || finish.Status != "failed" || finish.ErrMsg == "" {
		t.Fatalf("FinishRun = %+v, want run 4 failed with an error", finish)
	}
}

func TestRunService_ExecuteStartedTask_CanceledWhileQueued(t *testing.T) {
	store := &mocks.MockRunStore{StartQueuedRunResult: true}
	runner := &mocks.MockPipelineRunner{}
	svc := NewRunService(store, nil, runner)
	svc.SetMaxConcurrentRuns(1)
	if !svc.tryAcquireSlot() {
		t.Fatal("expected the single slot to be free")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var cleanups atomic.Int32

	svc.executeStartedTask(ctx, func() {}, false, 8, "p", startTaskProject(), "t", domain.RunnerOptions{
		SourcesPrepared:  true,
		WorkspaceCleanup: func() { cleanups.Add(1) },
	})

	if len(runner.TaskRunCalls) != 0 {
		t.Fatalf("task runner invoked for a run canceled while queued: %+v", runner.TaskRunCalls)
	}
	if cleanups.Load() != 1 {
		t.Fatalf("workspace cleanups = %d, want 1", cleanups.Load())
	}
	// Cancel owns the canceled transition for a queued run; nothing here
	// may write a verdict first.
	if len(store.FinishRunCalls) != 0 {
		t.Fatalf("FinishRunCalls = %+v, want none", store.FinishRunCalls)
	}
	// The slot this call never acquired stays with its holder.
	if svc.tryAcquireSlot() {
		t.Fatal("slot was released by a call that never acquired it")
	}
}

// Canceling a queued StartTask run records it canceled, with
// the actor and the canceled metric, rather than failed.
func TestRunService_StartTask_CancelWhileQueuedRecordsCanceled(t *testing.T) {
	store := &mocks.MockRunStore{StartQueuedRunResult: true, GetRunResult: &domain.Run{ID: 12, ProjectName: "p", Status: "canceled"}}
	runner := &mocks.MockPipelineRunner{}
	svc := NewRunService(store, nil, runner)
	rec := &fakeRecorder{}
	svc.Metrics = rec
	svc.SetMaxConcurrentRuns(1)
	if !svc.tryAcquireSlot() {
		t.Fatal("expected the single slot to be free")
	}

	queued, err := svc.StartTask(context.Background(), 12, "p", startTaskProject(), "t", domain.RunnerOptions{SourcesPrepared: true})
	if err != nil || !queued {
		t.Fatalf("StartTask = (%v, %v), want queued", queued, err)
	}
	if err := svc.Cancel(context.Background(), 12, "user:alice"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if len(store.FinishRunCalls) != 0 {
		t.Fatalf("FinishRunCalls = %+v, want none", store.FinishRunCalls)
	}
	if len(store.CancelRunCalls) != 1 || store.CancelRunCalls[0].CanceledBy != "user:alice" {
		t.Fatalf("CancelRunCalls = %+v, want one by user:alice", store.CancelRunCalls)
	}
	if len(runner.TaskRunCalls) != 0 {
		t.Fatalf("task runner invoked for a canceled queued run: %+v", runner.TaskRunCalls)
	}
	if got := rec.snapshot(); len(got) != 1 || got[0].status != "canceled" {
		t.Fatalf("recorded runs = %+v, want one canceled", got)
	}
}

func TestRunService_ExecuteStartedTask_KeepsCanceledRun(t *testing.T) {
	store := &mocks.MockRunStore{GetRunResult: &domain.Run{ID: 9, Status: "canceled"}}
	runner := &mocks.MockPipelineRunner{TaskResultList: []domain.TaskResult{{Name: "t", Status: "failed", Error: "killed"}}}
	svc := NewRunService(store, nil, runner)

	svc.executeStartedTask(context.Background(), func() {}, true, 9, "p", startTaskProject(), "t", domain.RunnerOptions{SourcesPrepared: true})

	if len(store.FinishRunCalls) != 0 {
		t.Fatalf("FinishRunCalls = %+v, want none (run already canceled)", store.FinishRunCalls)
	}
}

// A task that reports itself canceled finishes the run
// canceled, the same derivation finishRun uses, not passed.
func TestRunService_FinishStartedTask_CanceledTaskFinishesRunCanceled(t *testing.T) {
	store := &mocks.MockRunStore{}
	svc := NewRunService(store, nil, nil)

	svc.finishStartedTask(2, domain.TaskResult{Name: "t", Status: "canceled"}, nil, nil)

	if len(store.FinishRunCalls) != 1 {
		t.Fatalf("FinishRunCalls = %+v, want 1", store.FinishRunCalls)
	}
	if finish := store.FinishRunCalls[0]; finish.Status != "canceled" || finish.ErrMsg != "canceled" {
		t.Fatalf("FinishRun = %+v, want canceled", finish)
	}
}

func TestRunService_FinishStartedTask_LogsFinishRunError(t *testing.T) {
	logBuf := captureLog(t)
	store := &mocks.MockRunStore{FinishRunErr: errors.New("db down: hunter2-long-secret")}
	svc := NewRunService(store, nil, nil)

	svc.finishStartedTask(2, domain.TaskResult{Name: "t", Status: "passed"}, nil, []string{"hunter2-long-secret"})

	got := logBuf.String()
	if !strings.Contains(got, "could not finish run record") {
		t.Fatalf("log = %q, want the FinishRun warning", got)
	}
	if strings.Contains(got, "hunter2-long-secret") {
		t.Fatalf("log leaked a secret: %q", got)
	}
}

func TestRunService_FinishStartedTask_NilRunStoreIsNoop(t *testing.T) {
	svc := NewRunService(nil, nil, nil)
	svc.finishStartedTask(2, domain.TaskResult{Name: "t", Status: "passed"}, nil, nil)
}
