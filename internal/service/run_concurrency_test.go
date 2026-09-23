package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/model"
)

// TestRunService_Counts_UnlimitedConcurrencyReportsZeroRunning covers the
// nil-slots case: with no concurrency limit set, there is no capacity to
// measure occupancy against.
func TestRunService_Counts_UnlimitedConcurrencyReportsZeroRunning(t *testing.T) {
	store := &mocks.MockRunStore{CountByStatusResult: map[string]int{"queued": 3}}
	svc := NewRunService(store, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})

	running, queued := svc.Counts()
	if running != 0 {
		t.Errorf("running = %d, want 0 (unlimited concurrency)", running)
	}
	if queued != 3 {
		t.Errorf("queued = %d, want 3 (from CountRunsByStatus)", queued)
	}
}

// TestRunService_Counts_ReflectsOccupiedSlotsAndDrainsToZero covers
// tasks.md 4.2: queue runs past the concurrency limit, assert both gauges,
// then drain and assert zero.
func TestRunService_Counts_ReflectsOccupiedSlotsAndDrainsToZero(t *testing.T) {
	blocker := &blockingPipelineRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{})}
	store := &mocks.MockRunStore{}
	svc := NewRunService(store, blocker, blocker)
	svc.SetMaxConcurrentRuns(1)

	if running, queued := svc.Counts(); running != 0 || queued != 0 {
		t.Fatalf("before any run: Counts() = (%d, %d), want (0, 0)", running, queued)
	}

	_, err := svc.Start(context.Background(), "proj", &model.Project{Name: "proj", Tasks: []model.Task{{Name: "t"}}}, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-blocker.startedCh

	// The only slot is occupied by the run above; CountRunsByStatus is
	// stubbed to simulate one further run genuinely queued behind it.
	store.CountByStatusResult = map[string]int{"queued": 1}
	if running, queued := svc.Counts(); running != 1 || queued != 1 {
		t.Fatalf("while occupied: Counts() = (%d, %d), want (1, 1)", running, queued)
	}

	close(blocker.releaseCh)
	store.WaitFinish()
	store.CountByStatusResult = map[string]int{}
	// FinishRun (awaited above) runs inside executeRun, strictly before the
	// goroutine's deferred releaseSlot unwinds - so Counts().running settles
	// to 0 very shortly after WaitFinish returns, but not provably before
	// it; poll instead of asserting immediately.
	deadline := time.After(3 * time.Second)
	for {
		running, queued := svc.Counts()
		if running == 0 && queued == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("after drain: Counts() = (%d, %d), want (0, 0)", running, queued)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// TestRunService_Counts_RunStoreErrorReportsZeroQueued covers the
// CountRunsByStatus-error sub-branch: Counts must not propagate the error
// (it has no error return), and reports 0 rather than a stale or guessed
// value.
func TestRunService_Counts_RunStoreErrorReportsZeroQueued(t *testing.T) {
	store := &mocks.MockRunStore{CountByStatusErr: errors.New("count failed")}
	svc := NewRunService(store, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})

	if _, queued := svc.Counts(); queued != 0 {
		t.Errorf("queued = %d, want 0 on a CountRunsByStatus error", queued)
	}
}

// TestRunService_Counts_NilRunStoreReportsZeroQueued covers the
// runStore-nil guard.
func TestRunService_Counts_NilRunStoreReportsZeroQueued(t *testing.T) {
	svc := NewRunService(nil, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	if _, queued := svc.Counts(); queued != 0 {
		t.Errorf("queued = %d, want 0 with no run store", queued)
	}
}
