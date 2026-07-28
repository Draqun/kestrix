package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gitlab.com/lunaris-bot/metaci/internal/domain"
	"gitlab.com/lunaris-bot/metaci/internal/mocks"
	"gitlab.com/lunaris-bot/metaci/internal/model"
)

func TestRunService_Start_Happy(t *testing.T) {
	finishCh := make(chan struct{})
	mockRun := &mocks.MockRunStore{CreateRunID: 42, FinishRunCh: finishCh}
	mockRunner := &mocks.MockPipelineRunner{
		TaskResultList: []domain.TaskResult{
			{Name: "build", Status: "passed"},
		},
	}
	svc := NewRunService(mockRun, mockRunner)

	runID, err := svc.Start(context.Background(), "myproject", &model.Project{
		Name:  "myproject",
		Tasks: []model.Task{{Name: "build"}},
	}, []string{"main.go"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runID != 42 {
		t.Fatalf("expected run ID 42, got %d", runID)
	}
	if len(mockRun.CreateRunCalls) != 1 {
		t.Fatalf("expected 1 CreateRun call, got %d", len(mockRun.CreateRunCalls))
	}

	select {
	case <-finishCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for FinishRun")
	}
	if len(mockRun.FinishRunCalls) != 1 {
		t.Fatalf("expected 1 FinishRun call, got %d", len(mockRun.FinishRunCalls))
	}
	if mockRun.FinishRunCalls[0].Status != "passed" {
		t.Errorf("expected finish status 'passed', got %q", mockRun.FinishRunCalls[0].Status)
	}
}

func TestRunService_Start_TaskFailed(t *testing.T) {
	finishCh := make(chan struct{})
	mockRun := &mocks.MockRunStore{CreateRunID: 1, FinishRunCh: finishCh}
	mockRunner := &mocks.MockPipelineRunner{
		TaskResultList: []domain.TaskResult{
			{Name: "test", Status: "failed", Error: "exit 1"},
		},
	}
	svc := NewRunService(mockRun, mockRunner)

	_, err := svc.Start(context.Background(), "proj", &model.Project{
		Name:  "proj",
		Tasks: []model.Task{{Name: "test"}},
	}, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-finishCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for FinishRun")
	}
	if len(mockRun.FinishRunCalls) != 1 {
		t.Fatalf("expected 1 FinishRun call, got %d", len(mockRun.FinishRunCalls))
	}
	if mockRun.FinishRunCalls[0].Status != "failed" {
		t.Errorf("expected finish status 'failed', got %q", mockRun.FinishRunCalls[0].Status)
	}
}

func TestRunService_Start_PanicRecovered(t *testing.T) {
	finishCh := make(chan struct{})
	mockRun := &mocks.MockRunStore{CreateRunID: 1, FinishRunCh: finishCh}
	mockRunner := &mocks.MockPipelineRunner{PanicMsg: "boom"}
	svc := NewRunService(mockRun, mockRunner)

	_, err := svc.Start(context.Background(), "proj", &model.Project{
		Name:  "proj",
		Tasks: []model.Task{},
	}, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-finishCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for FinishRun")
	}
	if len(mockRun.FinishRunCalls) != 1 {
		t.Fatalf("expected 1 FinishRun call, got %d", len(mockRun.FinishRunCalls))
	}
	if mockRun.FinishRunCalls[0].Status != "failed" {
		t.Errorf("expected finish status 'failed' after panic, got %q", mockRun.FinishRunCalls[0].Status)
	}
}

func TestRunService_Start_CreateRunFails(t *testing.T) {
	mockRun := &mocks.MockRunStore{CreateRunErr: fmt.Errorf("db error")}
	mockRunner := &mocks.MockPipelineRunner{}
	svc := NewRunService(mockRun, mockRunner)

	_, err := svc.Start(context.Background(), "proj", &model.Project{
		Name:  "proj",
		Tasks: []model.Task{},
	}, nil, false)
	if err == nil {
		t.Fatal("expected error when CreateRun fails")
	}
}

func TestRunService_Start_RunnerError(t *testing.T) {
	finishCh := make(chan struct{})
	mockRun := &mocks.MockRunStore{CreateRunID: 1, FinishRunCh: finishCh}
	mockRunner := &mocks.MockPipelineRunner{RunErr: fmt.Errorf("exec failed")}
	svc := NewRunService(mockRun, mockRunner)

	_, err := svc.Start(context.Background(), "proj", &model.Project{
		Name:  "proj",
		Tasks: []model.Task{{Name: "build"}},
	}, []string{"main.go"}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-finishCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for FinishRun")
	}
	if len(mockRun.FinishRunCalls) != 1 {
		t.Fatalf("expected 1 FinishRun call, got %d", len(mockRun.FinishRunCalls))
	}
	if mockRun.FinishRunCalls[0].Status != "failed" {
		t.Errorf("expected finish status 'failed', got %q", mockRun.FinishRunCalls[0].Status)
	}
	if mockRun.FinishRunCalls[0].ErrMsg != "exec failed" {
		t.Errorf("expected error message 'exec failed', got %q", mockRun.FinishRunCalls[0].ErrMsg)
	}
}
