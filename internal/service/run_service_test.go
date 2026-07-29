package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gitlab.com/lunaris-bot/metaci/internal/domain"
	"gitlab.com/lunaris-bot/metaci/internal/mocks"
	"gitlab.com/lunaris-bot/metaci/internal/model"
)

type emptyTaskRunner struct{}

func (emptyTaskRunner) Run(ctx context.Context, project model.Project, changed map[string]bool, opts domain.RunnerOptions) ([]domain.TaskResult, error) {
	return nil, fmt.Errorf("boom")
}

func (emptyTaskRunner) RunTask(ctx context.Context, project model.Project, taskName string, opts domain.RunnerOptions) (domain.TaskResult, error) {
	return domain.TaskResult{}, fmt.Errorf("boom")
}

func TestRunService_Start_Happy(t *testing.T) {
	finishCh := make(chan struct{})
	mockRun := &mocks.MockRunStore{CreateRunID: 42, FinishRunCh: finishCh}
	mockRunner := &mocks.MockPipelineRunner{
		TaskResultList: []domain.TaskResult{
			{Name: "build", Status: "passed"},
		},
	}
	svc := NewRunService(mockRun, mockRunner, mockRunner)

	runID, err := svc.Start(context.Background(), "myproject", &model.Project{
		Name:  "myproject",
		Tasks: []model.Task{{Name: "build"}},
	}, []string{"main.go"}, domain.RunnerOptions{})
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
	svc := NewRunService(mockRun, mockRunner, mockRunner)

	_, err := svc.Start(context.Background(), "proj", &model.Project{
		Name:  "proj",
		Tasks: []model.Task{{Name: "test"}},
	}, nil, domain.RunnerOptions{})
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
	svc := NewRunService(mockRun, mockRunner, mockRunner)

	_, err := svc.Start(context.Background(), "proj", &model.Project{
		Name:  "proj",
		Tasks: []model.Task{},
	}, nil, domain.RunnerOptions{})
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
	svc := NewRunService(mockRun, mockRunner, mockRunner)

	_, err := svc.Start(context.Background(), "proj", &model.Project{
		Name:  "proj",
		Tasks: []model.Task{},
	}, nil, domain.RunnerOptions{})
	if err == nil {
		t.Fatal("expected error when CreateRun fails")
	}
}

func TestRunService_Start_RunnerError(t *testing.T) {
	finishCh := make(chan struct{})
	mockRun := &mocks.MockRunStore{CreateRunID: 1, FinishRunCh: finishCh}
	mockRunner := &mocks.MockPipelineRunner{RunErr: fmt.Errorf("exec failed")}
	svc := NewRunService(mockRun, mockRunner, mockRunner)

	_, err := svc.Start(context.Background(), "proj", &model.Project{
		Name:  "proj",
		Tasks: []model.Task{{Name: "build"}},
	}, []string{"main.go"}, domain.RunnerOptions{IncludeManual: true})
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

func TestRunService_Start_RejectsNilPipeline(t *testing.T) {
	svc := NewRunService(nil, nil, nil)
	_, err := svc.Start(context.Background(), "proj", &model.Project{Name: "proj"}, nil, domain.RunnerOptions{})
	if err == nil {
		t.Fatal("expected error for nil pipeline runner")
	}
}

func TestRunService_Start_RejectsNilProject(t *testing.T) {
	svc := NewRunService(nil, &emptyTaskRunner{}, &emptyTaskRunner{})
	_, err := svc.Start(context.Background(), "proj", nil, nil, domain.RunnerOptions{})
	if err == nil {
		t.Fatal("expected error for nil project")
	}
}

func TestRunService_RunTask_RejectsNilTaskRunner(t *testing.T) {
	svc := NewRunService(nil, &emptyTaskRunner{}, nil)
	_, err := svc.RunTask(context.Background(), 0, "proj", &model.Project{Name: "proj"}, "manual", domain.RunnerOptions{})
	if err == nil {
		t.Fatal("expected error for nil task runner")
	}
}

func TestRunService_NewRunService_RequiresExplicitTaskRunner(t *testing.T) {
	mockRunner := &mocks.MockPipelineRunner{}
	svc := NewRunService(nil, mockRunner, mockRunner)
	if svc == nil {
		t.Fatal("NewRunService returned nil")
	}
	if svc.taskRunner == nil {
		t.Fatal("expected taskRunner to be set explicitly")
	}
}

func TestRunService_RunTask_Success(t *testing.T) {
	mockRun := &mocks.MockRunStore{ApplyTaskCh: make(chan struct{})}
	mockRunner := &mocks.MockPipelineRunner{
		TaskResultList: []domain.TaskResult{{Name: "manual", Status: "passed", Output: "ok"}},
	}
	svc := NewRunService(mockRun, mockRunner, mockRunner)

	tr, err := svc.RunTask(context.Background(), 7, "proj", &model.Project{Name: "proj", Tasks: []model.Task{{Name: "manual"}}}, "manual", domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.Status != "passed" || tr.Output != "ok" {
		t.Fatalf("unexpected task result: %+v", tr)
	}
	if len(mockRun.ApplyTaskCalls) != 2 {
		t.Fatalf("expected 2 ApplyTaskResult calls, got %d", len(mockRun.ApplyTaskCalls))
	}
}

func TestRunService_RunTask_ErrorWithoutStatus(t *testing.T) {
	mockRun := &mocks.MockRunStore{}
	svc := NewRunService(mockRun, &emptyTaskRunner{}, &emptyTaskRunner{})

	tr, err := svc.RunTask(context.Background(), 0, "proj", &model.Project{Name: "proj"}, "manual", domain.RunnerOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if tr.Status != "failed" || tr.Error != "boom" {
		t.Fatalf("unexpected task result: %+v", tr)
	}
}

func TestRunService_ApproveManualTask_Happy(t *testing.T) {
	mockRun := &mocks.MockRunStore{
		GetRunResult: &domain.Run{
			ID:          7,
			ProjectName: "proj",
			Status:      "running",
			PipelineDef: &model.Project{
				Name: "proj",
				Tasks: []model.Task{
					{Name: "build"},
					{Name: "manual", Manual: true, DependsOn: []string{"build"}},
				},
			},
			TaskResults: []domain.TaskResult{
				{Name: "build", Status: "passed"},
				{Name: "manual", Status: "manual"},
			},
		},
	}
	mockRunner := &mocks.MockPipelineRunner{
		TaskResultList: []domain.TaskResult{{Name: "manual", Status: "passed", Output: "ok"}},
	}
	svc := NewRunService(mockRun, mockRunner, mockRunner)

	tr, err := svc.ApproveManualTask(context.Background(), 7, "proj", &model.Project{
		Name: "proj",
		Tasks: []model.Task{
			{Name: "build"},
			{Name: "manual", Manual: true, DependsOn: []string{"build"}},
		},
	}, "manual", domain.RunnerOptions{WorkDir: "/repo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.Status != "passed" || tr.Output != "ok" {
		t.Fatalf("unexpected task result: %+v", tr)
	}
	if len(mockRun.ApplyTaskCalls) != 2 {
		t.Fatalf("expected 2 ApplyTaskResult calls, got %d", len(mockRun.ApplyTaskCalls))
	}
}

func TestProjectService_Update_GetError(t *testing.T) {
	svc := NewProjectService(&mocks.MockProjectStore{GetErr: errors.New("boom")})
	if err := svc.Update("demo", &domain.ProjectConfig{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunService_createRun_NoStore(t *testing.T) {
	svc := NewRunService(nil, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	runID, err := svc.createRun("proj", nil, false, &model.Project{Name: "proj"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runID != 0 {
		t.Fatalf("runID = %d, want 0", runID)
	}
}

func TestRunService_prepareRunExecution_InvokesCallbacks(t *testing.T) {
	mockRun := &mocks.MockRunStore{}
	svc := NewRunService(mockRun, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	out := &bytes.Buffer{}
	opts := domain.RunnerOptions{}
	svc.prepareRunExecution(9, out, &opts)
	if opts.Stdout != out || opts.Stderr != out {
		t.Fatal("expected stdio to be wired to buffer")
	}
	if opts.OnTaskStart == nil || opts.OnTaskComplete == nil {
		t.Fatal("expected callbacks to be set")
	}
	opts.OnTaskStart("build")
	opts.OnTaskComplete(domain.TaskResult{Name: "build", Status: "passed", Output: "ok"})
	if len(mockRun.AppendTaskCalls) != 2 {
		t.Fatalf("AppendTaskCalls = %d, want 2", len(mockRun.AppendTaskCalls))
	}
	if len(mockRun.FlushRunCalls) != 1 {
		t.Fatalf("FlushRunCalls = %d, want 1", len(mockRun.FlushRunCalls))
	}
}

func TestRunService_RunTask_NilProject(t *testing.T) {
	svc := NewRunService(nil, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	if _, err := svc.RunTask(context.Background(), 0, "proj", nil, "manual", domain.RunnerOptions{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestTaskResultsByName_NilRun(t *testing.T) {
	results := taskResultsByName(nil)
	if len(results) != 0 {
		t.Fatalf("results = %v, want empty", results)
	}
}

func TestManualTaskContext_ProjectOverridesDependsOn(t *testing.T) {
	run := &domain.Run{PipelineDef: &model.Project{Tasks: []model.Task{{Name: "manual", Manual: true, DependsOn: []string{"build"}}}}}
	proj := &model.Project{Tasks: []model.Task{{Name: "manual", Manual: true, DependsOn: []string{"other"}}}}
	manualOK, dependsOn := manualTaskContext(run, proj, "manual")
	if !manualOK {
		t.Fatal("expected manual task")
	}
	if len(dependsOn) != 1 || dependsOn[0] != "build" {
		t.Fatalf("dependsOn = %v, want [build]", dependsOn)
	}
}

func TestApproveManualTask_ErrorBranches(t *testing.T) {
	proj := &model.Project{Name: "proj", Tasks: []model.Task{{Name: "manual", Manual: true}}}
	tests := []struct {
		name string
		svc  *RunService
		proj *model.Project
		err  string
	}{
		{name: "no store", svc: NewRunService(nil, nil, nil), err: "runs not supported"},
		{name: "get run error", svc: NewRunService(&mocks.MockRunStore{GetRunErr: errors.New("boom")}, nil, nil), err: "get run"},
		{name: "run not found", svc: NewRunService(&mocks.MockRunStore{GetRunResult: &domain.Run{ProjectName: "other"}}, nil, nil), err: "run not found"},
		{name: "task not in run", svc: NewRunService(&mocks.MockRunStore{GetRunResult: &domain.Run{ProjectName: "proj", TaskResults: []domain.TaskResult{{Name: "build", Status: "passed"}}}}, nil, nil), err: "task not in run"},
		{name: "not waiting", svc: NewRunService(&mocks.MockRunStore{GetRunResult: &domain.Run{ProjectName: "proj", TaskResults: []domain.TaskResult{{Name: "manual", Status: "passed"}}}}, nil, nil), err: "not waiting"},
		{name: "not manual", svc: NewRunService(&mocks.MockRunStore{GetRunResult: &domain.Run{ProjectName: "proj", TaskResults: []domain.TaskResult{{Name: "manual", Status: "manual"}}, PipelineDef: &model.Project{Tasks: []model.Task{{Name: "manual"}}}}}, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{}), proj: &model.Project{Name: "proj", Tasks: []model.Task{{Name: "manual"}}}, err: "task is not manual"},
		{name: "dependency missing", svc: NewRunService(&mocks.MockRunStore{GetRunResult: &domain.Run{ProjectName: "proj", TaskResults: []domain.TaskResult{{Name: "manual", Status: "manual"}}, PipelineDef: &model.Project{Tasks: []model.Task{{Name: "manual", Manual: true, DependsOn: []string{"build"}}}}}}, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{}), proj: proj, err: "dependency \"build\" has not passed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.svc.ApproveManualTask(context.Background(), 1, "proj", tt.proj, "manual", domain.RunnerOptions{})
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Fatalf("err = %v, want contains %q", err, tt.err)
			}
		})
	}
}

func TestWebhookService_Process_NilAndAppend(t *testing.T) {
	var nilSvc *WebhookService
	if err := nilSvc.Process(map[string]interface{}{"a": 1}); err != nil {
		t.Fatalf("nil receiver returned error: %v", err)
	}
	svc := NewWebhookService(&mocks.MockWebhookLogSink{})
	if err := svc.Process(map[string]interface{}{"event": "push"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
