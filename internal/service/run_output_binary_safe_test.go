package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/model"
)

// TestRunService_FinishRun_LogsFailedWriteAndReleasesSlot pins task 3.1:
// finishRun logs a failed FinishRun, redacts secrets from the error,
// and the run is still unregistered and its slot released.
func TestRunService_FinishRun_LogsFailedWriteAndReleasesSlot(t *testing.T) {
	logBuf := captureLog(t)
	secret := "super-secret-token-12345"
	finishCh := make(chan struct{})

	mockRun := &mocks.MockRunStore{
		CreateRunID:  42,
		FinishRunCh:  finishCh,
		FinishRunErr: errors.New("connection failed: " + secret),
	}
	mockRunner := &mocks.MockPipelineRunner{
		TaskResultList: []domain.TaskResult{
			{Name: "build", Status: "passed"},
		},
	}
	svc := NewRunService(mockRun, mockRunner, mockRunner)
	svc.SetMaxConcurrentRuns(1)

	runID, err := svc.Start(context.Background(), "test-proj", &model.Project{
		Name:  "test-proj",
		Tasks: []model.Task{{Name: "build"}},
	}, nil, domain.RunnerOptions{
		ResolvedSecretValues: []string{secret},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case <-finishCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for FinishRun")
	}

	// Wait for goroutine to finish and unregister
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		svc.mu.Lock()
		_, active := svc.active[runID]
		svc.mu.Unlock()
		if !active {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	svc.mu.Lock()
	_, stillActive := svc.active[runID]
	svc.mu.Unlock()
	if stillActive {
		t.Fatalf("run %d still active in svc.active after failed FinishRun", runID)
	}

	// Slot must be released so next run acquires without queueing
	if acquired := svc.markQueuedIfBusy(999); !acquired {
		t.Fatalf("slot was not released after failed FinishRun")
	}

	// Verify log warning
	logStr := logBuf.String()
	if !strings.Contains(logStr, "could not finish run record") {
		t.Fatalf("log = %q, want 'could not finish run record'", logStr)
	}
	if !strings.Contains(logStr, "run_id=42") {
		t.Fatalf("log = %q, want run_id=42", logStr)
	}
	if strings.Contains(logStr, secret) {
		t.Fatalf("log leaked secret %q: %q", secret, logStr)
	}
	if !strings.Contains(logStr, "***") {
		t.Fatalf("log = %q, want redacted secret marker '***'", logStr)
	}
}

// TestRunService_OnTaskComplete_LogsFailedAppendTaskResult pins task 3.1:
// OnTaskComplete logs a failed AppendTaskResult with secret redacted.
func TestRunService_OnTaskComplete_LogsFailedAppendTaskResult(t *testing.T) {
	logBuf := captureLog(t)
	secret := "secret-in-append-err-999"
	finishCh := make(chan struct{})

	mockRun := &mocks.MockRunStore{
		CreateRunID:   42,
		FinishRunCh:   finishCh,
		AppendTaskErr: errors.New("db disk full: " + secret),
	}
	mockRunner := &echoingPipelineRunner{
		taskName: "build",
		output:   "build log",
	}
	svc := NewRunService(mockRun, mockRunner, mockRunner)

	_, err := svc.Start(context.Background(), "test-proj", &model.Project{
		Name:  "test-proj",
		Tasks: []model.Task{{Name: "build"}},
	}, nil, domain.RunnerOptions{
		ResolvedSecretValues: []string{secret},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case <-finishCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for FinishRun")
	}

	logStr := logBuf.String()
	if !strings.Contains(logStr, "could not append task result") {
		t.Fatalf("log = %q, want 'could not append task result'", logStr)
	}
	if !strings.Contains(logStr, "run_id=42") {
		t.Fatalf("log = %q, want run_id=42", logStr)
	}
	if strings.Contains(logStr, secret) {
		t.Fatalf("log leaked secret %q: %q", secret, logStr)
	}
	if !strings.Contains(logStr, "***") {
		t.Fatalf("log = %q, want redacted secret marker '***'", logStr)
	}
}

// TestRunService_OnTaskComplete_LogsFailedFlushRunOutput pins task 3.1:
// OnTaskComplete logs a failed FlushRunOutput with secret redacted.
func TestRunService_OnTaskComplete_LogsFailedFlushRunOutput(t *testing.T) {
	logBuf := captureLog(t)
	secret := "secret-in-flush-err-888"
	finishCh := make(chan struct{})

	mockRun := &mocks.MockRunStore{
		CreateRunID: 42,
		FinishRunCh: finishCh,
		FlushRunErr: errors.New("write failed: " + secret),
	}
	mockRunner := &echoingPipelineRunner{
		taskName: "build",
		output:   "build log",
	}
	svc := NewRunService(mockRun, mockRunner, mockRunner)

	_, err := svc.Start(context.Background(), "test-proj", &model.Project{
		Name:  "test-proj",
		Tasks: []model.Task{{Name: "build"}},
	}, nil, domain.RunnerOptions{
		ResolvedSecretValues: []string{secret},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case <-finishCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for FinishRun")
	}

	logStr := logBuf.String()
	if !strings.Contains(logStr, "could not flush run output") {
		t.Fatalf("log = %q, want 'could not flush run output'", logStr)
	}
	if !strings.Contains(logStr, "run_id=42") {
		t.Fatalf("log = %q, want run_id=42", logStr)
	}
	if strings.Contains(logStr, secret) {
		t.Fatalf("log leaked secret %q: %q", secret, logStr)
	}
	if !strings.Contains(logStr, "***") {
		t.Fatalf("log = %q, want redacted secret marker '***'", logStr)
	}
}
