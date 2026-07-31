package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/model"
)

type emptyTaskRunner struct{}

type sourceSessionTracker struct {
	pins      [][]domain.AcquiredSource
	releases  []int
	releaseCh chan struct{}
	pinErr    error
}

func (s *sourceSessionTracker) Open(context.Context, domain.SourceRequest) (domain.SourceSession, error) {
	return domain.SourceSession{}, nil
}
func (s *sourceSessionTracker) Pin(_ context.Context, _ int, sources []domain.AcquiredSource) error {
	s.pins = append(s.pins, append([]domain.AcquiredSource(nil), sources...))
	return s.pinErr
}
func (s *sourceSessionTracker) Release(_ context.Context, runID int) error {
	s.releases = append(s.releases, runID)
	if s.releaseCh != nil {
		close(s.releaseCh)
	}
	return nil
}
func (s *sourceSessionTracker) Maintain(context.Context) error { return nil }

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

func TestRunServicePersistsPinsAndRetainsSourcesForManualWork(t *testing.T) {
	store := &mocks.MockRunStore{CreateRunID: 51, FinishRunCh: make(chan struct{})}
	additional := domain.AcquiredSource{RepoName: "docs", URL: "https://git.test/docs.git", RequestedRef: "main", ResolvedRevision: "docs-sha"}
	runner := &mocks.MockPipelineRunner{
		AcquiredSources: []domain.AcquiredSource{additional},
		TaskResultList:  []domain.TaskResult{{Name: "deploy", Status: "manual"}},
	}
	manager := &sourceSessionTracker{}
	primary := domain.AcquiredSource{RepoName: "app", URL: "https://git.test/app.git", RequestedRef: "main", ResolvedRevision: "app-sha", Primary: true}
	svc := NewRunService(store, runner, runner)
	_, err := svc.Start(context.Background(), "app", &model.Project{Name: "app", Tasks: []model.Task{{Name: "deploy", Manual: true}}}, nil, domain.RunnerOptions{
		SourceSessions: manager, InitialSources: []domain.AcquiredSource{primary},
	})
	if err != nil {
		t.Fatal(err)
	}
	store.WaitFinish()
	if len(store.UpdateSourcesCalls) != 1 || len(store.UpdateSourcesCalls[0]) != 2 {
		t.Fatalf("persisted sources = %+v", store.UpdateSourcesCalls)
	}
	if len(manager.pins) != 1 || len(manager.pins[0]) != 2 {
		t.Fatalf("pins = %+v", manager.pins)
	}
	if len(manager.releases) != 0 {
		t.Fatalf("manual run released pins: %v", manager.releases)
	}
	if len(store.FinishRunCalls) != 1 || store.FinishRunCalls[0].Status != "running" {
		t.Fatalf("finish calls = %+v", store.FinishRunCalls)
	}
}

func TestRunServiceReleasesSourcesWhenRunIsTerminal(t *testing.T) {
	store := &mocks.MockRunStore{CreateRunID: 52, FinishRunCh: make(chan struct{})}
	runner := &mocks.MockPipelineRunner{TaskResultList: []domain.TaskResult{{Name: "build", Status: "passed"}}}
	manager := &sourceSessionTracker{releaseCh: make(chan struct{})}
	svc := NewRunService(store, runner, runner)
	_, err := svc.Start(context.Background(), "app", &model.Project{Name: "app", Tasks: []model.Task{{Name: "build"}}}, nil, domain.RunnerOptions{SourceSessions: manager})
	if err != nil {
		t.Fatal(err)
	}
	store.WaitFinish()
	<-manager.releaseCh
	if len(manager.releases) != 1 || manager.releases[0] != 52 {
		t.Fatalf("releases = %v", manager.releases)
	}
}

func TestRunServicePanicReleasesSourcePins(t *testing.T) {
	store := &mocks.MockRunStore{CreateRunID: 53, FinishRunCh: make(chan struct{})}
	runner := &mocks.MockPipelineRunner{PanicMsg: "panic"}
	manager := &sourceSessionTracker{releaseCh: make(chan struct{})}
	svc := NewRunService(store, runner, runner)
	if _, err := svc.Start(context.Background(), "app", &model.Project{Name: "app"}, nil, domain.RunnerOptions{SourceSessions: manager}); err != nil {
		t.Fatal(err)
	}
	store.WaitFinish()
	<-manager.releaseCh
	if len(manager.releases) != 1 || manager.releases[0] != 53 {
		t.Fatalf("releases = %v", manager.releases)
	}
}

func TestRunStatusRemainsRunningWhileManualWorkCanStillExecute(t *testing.T) {
	status, errMsg := runStatus([]domain.TaskResult{
		{Name: "test", Status: "failed", Error: "exit 1"},
		{Name: "deploy", Status: "manual"},
	}, nil)

	if status != "running" {
		t.Fatalf("status = %q, want running", status)
	}
	if errMsg != "exit 1" {
		t.Fatalf("error = %q, want preserved task failure", errMsg)
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

func TestProjectServiceValidatedSourceFailuresDoNotPersist(t *testing.T) {
	store := &mocks.MockProjectStore{Projects: map[string]*domain.ProjectConfig{
		"demo": {Name: "demo", RepoURL: "https://git.test/old.git", RepoRef: "main"},
	}}
	svc := NewProjectService(store)
	validationErr := errors.New("remote denied")
	validate := func(*domain.ProjectConfig) error { return validationErr }

	if err := svc.CreateValidated(&domain.ProjectConfig{Name: "new", RepoURL: "https://git.test/new.git", RepoRef: "main"}, validate); !errors.Is(err, ErrValidation) {
		t.Fatalf("create error = %v", err)
	}
	if _, exists := store.Projects["new"]; exists {
		t.Fatal("failed create was persisted")
	}
	if err := svc.UpdateValidated("demo", &domain.ProjectConfig{RepoURL: "https://git.test/new.git", RepoRef: "main"}, validate); !errors.Is(err, ErrValidation) {
		t.Fatalf("update error = %v", err)
	}
	if store.Projects["demo"].RepoURL != "https://git.test/old.git" {
		t.Fatal("failed update replaced existing project")
	}
}

func TestRunService_createRun_NoStore(t *testing.T) {
	svc := NewRunService(nil, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	runID, err := svc.createRun("proj", nil, false, &model.Project{Name: "proj"})
	if err == nil || err.Error() != "run store is not configured" {
		t.Fatalf("error = %v, want missing run store error", err)
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
	opts.OnSourcesAcquired([]domain.AcquiredSource{{RepoName: "repo"}})
	withSources := &runSourceMock{MockRunStore: &mocks.MockRunStore{}}
	NewRunService(withSources, nil, nil).prepareRunExecution(10, &bytes.Buffer{}, &domain.RunnerOptions{})
	// The callback is installed by prepareRunExecution and reaches the optional
	// run-source persistence port.
	var sourceOpts domain.RunnerOptions
	NewRunService(withSources, nil, nil).prepareRunExecution(10, &bytes.Buffer{}, &sourceOpts)
	sourceOpts.OnSourcesAcquired([]domain.AcquiredSource{{RepoName: "repo"}})
	if withSources.sourceCalls != 1 {
		t.Fatalf("source calls=%d", withSources.sourceCalls)
	}
}

func TestRunServicePrepareRunExecutionSourceCallbackFailures(t *testing.T) {
	persistStore := &runSourceMock{MockRunStore: &mocks.MockRunStore{}, sourceErr: errors.New("persist failed")}
	var persistOpts domain.RunnerOptions
	NewRunService(persistStore, nil, nil).prepareRunExecution(1, &bytes.Buffer{}, &persistOpts)
	if err := persistOpts.OnSourcesAcquired(nil); err == nil || !strings.Contains(err.Error(), "persist run sources") {
		t.Fatalf("persist error = %v", err)
	}

	manager := &sourceSessionTracker{pinErr: errors.New("pin failed")}
	var pinOpts = domain.RunnerOptions{SourceSessions: manager}
	NewRunService(&mocks.MockRunStore{}, nil, nil).prepareRunExecution(2, &bytes.Buffer{}, &pinOpts)
	if err := pinOpts.OnSourcesAcquired(nil); err == nil || !strings.Contains(err.Error(), "pin run sources") {
		t.Fatalf("pin error = %v", err)
	}
	if len(manager.releases) != 1 || manager.releases[0] != 2 {
		t.Fatalf("pin rollback releases = %v", manager.releases)
	}

	previousErr := errors.New("previous callback failed")
	previousOpts := domain.RunnerOptions{OnSourcesAcquired: func([]domain.AcquiredSource) error { return previousErr }}
	NewRunService(&mocks.MockRunStore{}, nil, nil).prepareRunExecution(3, &bytes.Buffer{}, &previousOpts)
	if err := previousOpts.OnSourcesAcquired(nil); !errors.Is(err, previousErr) {
		t.Fatalf("previous callback error = %v", err)
	}
}

type runSourceMock struct {
	*mocks.MockRunStore
	sourceCalls int
	sourceErr   error
}

func (m *runSourceMock) UpdateRunSources(int, []domain.AcquiredSource) error {
	m.sourceCalls++
	return m.sourceErr
}

func TestRunServiceRunTaskSourceLifecycleBranches(t *testing.T) {
	t.Run("workspace cleanup", func(t *testing.T) {
		cleaned := 0
		runner := &mocks.MockPipelineRunner{}
		_, err := NewRunService(nil, runner, runner).RunTask(context.Background(), 1, "demo", &model.Project{Name: "demo"}, "task", domain.RunnerOptions{
			SourcesPrepared: true, WorkspaceCleanup: func() { cleaned++ },
		})
		if err != nil || cleaned != 1 {
			t.Fatalf("error=%v cleaned=%d", err, cleaned)
		}
	})

	t.Run("persist source failure", func(t *testing.T) {
		store := &runSourceMock{MockRunStore: &mocks.MockRunStore{}, sourceErr: errors.New("persist failed")}
		runner := &mocks.MockPipelineRunner{}
		_, err := NewRunService(store, runner, runner).RunTask(context.Background(), 2, "demo", &model.Project{Name: "demo"}, "task", domain.RunnerOptions{})
		if err == nil || !strings.Contains(err.Error(), "persist run sources") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("pin failure rolls back", func(t *testing.T) {
		manager := &sourceSessionTracker{pinErr: errors.New("pin failed")}
		runner := &mocks.MockPipelineRunner{}
		_, err := NewRunService(&mocks.MockRunStore{}, runner, runner).RunTask(context.Background(), 3, "demo", &model.Project{Name: "demo"}, "task", domain.RunnerOptions{SourceSessions: manager})
		if err == nil || !strings.Contains(err.Error(), "pin run sources") || len(manager.releases) != 1 {
			t.Fatalf("error=%v releases=%v", err, manager.releases)
		}
	})

	t.Run("previous callback failure", func(t *testing.T) {
		previousErr := errors.New("callback failed")
		runner := &mocks.MockPipelineRunner{}
		_, err := NewRunService(&mocks.MockRunStore{}, runner, runner).RunTask(context.Background(), 4, "demo", &model.Project{Name: "demo"}, "task", domain.RunnerOptions{
			OnSourcesAcquired: func([]domain.AcquiredSource) error { return previousErr },
		})
		if err == nil || !strings.Contains(err.Error(), previousErr.Error()) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("terminal run releases pins", func(t *testing.T) {
		store := &mocks.MockRunStore{GetRunResult: &domain.Run{TaskResults: []domain.TaskResult{{Name: "task", Status: "passed"}}}}
		manager := &sourceSessionTracker{}
		runner := &mocks.MockPipelineRunner{}
		_, err := NewRunService(store, runner, runner).RunTask(context.Background(), 5, "demo", &model.Project{Name: "demo"}, "task", domain.RunnerOptions{
			SourcesPrepared: true, SourceSessions: manager,
		})
		if err != nil || len(manager.releases) != 1 || manager.releases[0] != 5 {
			t.Fatalf("error=%v releases=%v", err, manager.releases)
		}
	})
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
