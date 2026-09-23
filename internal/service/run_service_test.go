package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/model"
)

// fakeRecorder is a minimal metrics.Recorder double: it just remembers every
// call, so a test can assert RunService recorded exactly the status,
// project, and (roughly) duration it expects without depending on the real
// internal/metrics package's internals.
type fakeRecorder struct {
	mu    sync.Mutex
	runs  []fakeRunFinished
	hooks []string
}

type fakeRunFinished struct {
	status   string
	project  string
	duration time.Duration
}

func (f *fakeRecorder) RunFinished(status, project string, d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, fakeRunFinished{status: status, project: project, duration: d})
}

func (f *fakeRecorder) WebhookDelivery(outcome string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hooks = append(f.hooks, outcome)
}

func (f *fakeRecorder) snapshot() []fakeRunFinished {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeRunFinished(nil), f.runs...)
}

// captureLog redirects the standard logger to a buffer for the rest of the
// test, restoring it on cleanup. Tests using this must not run with
// t.Parallel(), since the standard logger is a package-level global.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })
	return &buf
}

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

func TestRunService_Start_RecordsAndLogsActor(t *testing.T) {
	buf := captureLog(t)
	finishCh := make(chan struct{})
	mockRun := &mocks.MockRunStore{CreateRunID: 42, FinishRunCh: finishCh}
	mockRunner := &mocks.MockPipelineRunner{
		TaskResultList: []domain.TaskResult{{Name: "build", Status: "passed"}},
	}
	svc := NewRunService(mockRun, mockRunner, mockRunner)
	rec := &fakeRecorder{}
	svc.Metrics = rec

	_, err := svc.Start(context.Background(), "myproject", &model.Project{
		Name:  "myproject",
		Tasks: []model.Task{{Name: "build"}},
	}, []string{"main.go"}, domain.RunnerOptions{Actor: "user:alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mockRun.CreateRunCalls) != 1 || mockRun.CreateRunCalls[0].StartedBy != "user:alice" {
		t.Fatalf("CreateRunCalls = %+v, want StartedBy=user:alice", mockRun.CreateRunCalls)
	}

	out := buf.String()
	if !strings.Contains(out, "run started") || !strings.Contains(out, "account=user:alice") ||
		!strings.Contains(out, "project=myproject") || !strings.Contains(out, "run_id=42") {
		t.Fatalf("log output = %q, want a started record naming the account, project, and run_id", out)
	}
	if strings.Contains(out, "main.go") {
		t.Fatalf("log output leaked request data beyond account/project/run_id: %q", out)
	}

	select {
	case <-finishCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for FinishRun")
	}
	runs := rec.snapshot()
	if len(runs) != 1 {
		t.Fatalf("expected 1 recorded run, got %d: %+v", len(runs), runs)
	}
	if runs[0].status != "passed" || runs[0].project != "myproject" {
		t.Errorf("recorded run = %+v, want status=passed project=myproject", runs[0])
	}
	if runs[0].duration < 0 {
		t.Errorf("recorded duration = %v, want non-negative", runs[0].duration)
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
	rec := &fakeRecorder{}
	svc.Metrics = rec
	_, err := svc.Start(context.Background(), "app", &model.Project{Name: "app", Tasks: []model.Task{{Name: "deploy", Manual: true}}}, nil, domain.RunnerOptions{
		SourceSessions: manager, InitialSources: []domain.AcquiredSource{primary},
	})
	if err != nil {
		t.Fatal(err)
	}
	store.WaitFinish()
	// A "running" status (a manual task is still pending) is not terminal -
	// see observability spec.md "Run outcome counted once" - so it must not
	// be recorded as a finished run.
	if runs := rec.snapshot(); len(runs) != 0 {
		t.Fatalf("recorded runs = %+v, want none for a non-terminal 'running' status", runs)
	}
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

func TestRunStatusCanceledTaskMakesTheRunCanceled(t *testing.T) {
	status, errMsg := runStatus([]domain.TaskResult{
		{Name: "victim", Status: "canceled", Error: "canceled"},
		{Name: "pending", Status: "canceled", Error: "canceled"},
	}, nil)
	if status != "canceled" {
		t.Fatalf("status = %q, want canceled", status)
	}
	if errMsg != "canceled" {
		t.Fatalf("error = %q, want canceled", errMsg)
	}
}

func TestRunStatusCanceledTaskOutranksAFailedTask(t *testing.T) {
	// A task killed by our own cancellation reports "canceled"; an
	// unrelated task that failed on its own still reports "failed" - but
	// the overall run is canceled, not failed, since that reflects operator
	// intent rather than a pipeline bug. See design.md D5.
	status, errMsg := runStatus([]domain.TaskResult{
		{Name: "unrelated", Status: "failed", Error: "exit 1"},
		{Name: "victim", Status: "canceled", Error: "canceled"},
	}, nil)
	if status != "canceled" {
		t.Fatalf("status = %q, want canceled", status)
	}
	if errMsg != "canceled" {
		t.Fatalf("error = %q, want canceled", errMsg)
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
	rec := &fakeRecorder{}
	svc.Metrics = rec

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
	// The panic-recovery path records its own metric directly (it returns
	// before executeRun's normal finishRun call), so this is a separate
	// assertion from the metric recorded on the ordinary completion path.
	runs := rec.snapshot()
	if len(runs) != 1 || runs[0].status != "failed" || runs[0].project != "proj" {
		t.Fatalf("recorded runs = %+v, want exactly one failed/proj entry", runs)
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

// REVIEW.md R1: task runners build TaskResult.Output as their own complete
// buffer, independent of the run's aggregate output stream that
// prepareRunExecution's boundedWriter bounds - so RunTask must separately
// truncate tr.Output before it reaches ApplyTaskResult, or a single verbose
// task stores unbounded output regardless of runner.max_output_bytes.
func TestRunService_RunTask_TruncatesOversizedTaskOutput(t *testing.T) {
	mockRun := &mocks.MockRunStore{}
	bigOutput := strings.Repeat("x", 100)
	mockRunner := &mocks.MockPipelineRunner{
		TaskResultList: []domain.TaskResult{{Name: "t", Status: "passed", Output: bigOutput}},
	}
	svc := NewRunService(mockRun, mockRunner, mockRunner)
	svc.MaxOutputBytes = 10

	tr, err := svc.RunTask(context.Background(), 1, "proj", &model.Project{Name: "proj"}, "t", domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	marker := "--- output truncated at 10 bytes (runner.max_output_bytes) ---\n"
	if !strings.HasSuffix(tr.Output, marker) {
		t.Fatalf("tr.Output = %q, want it to end with the truncation marker", tr.Output)
	}
	if len(tr.Output) > 10+len(marker) {
		t.Fatalf("tr.Output len = %d, want at most bound+marker", len(tr.Output))
	}
	last := mockRun.ApplyTaskCalls[len(mockRun.ApplyTaskCalls)-1]
	if !strings.HasSuffix(last.TaskResult.Output, marker) {
		t.Fatalf("persisted ApplyTaskResult output was not truncated: %+v", last)
	}
}

func TestRunService_RunTask_StampsApproverOnTerminalEntry(t *testing.T) {
	mockRun := &mocks.MockRunStore{ApplyTaskCh: make(chan struct{})}
	mockRunner := &mocks.MockPipelineRunner{
		TaskResultList: []domain.TaskResult{{Name: "manual", Status: "passed", Output: "ok"}},
	}
	svc := NewRunService(mockRun, mockRunner, mockRunner)

	before := time.Now()
	tr, err := svc.RunTask(context.Background(), 7, "proj", &model.Project{Name: "proj", Tasks: []model.Task{{Name: "manual"}}}, "manual", domain.RunnerOptions{Actor: "user:alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.ApprovedBy != "user:alice" {
		t.Fatalf("terminal entry ApprovedBy = %q, want user:alice", tr.ApprovedBy)
	}
	if tr.ApprovedAt == nil || tr.ApprovedAt.Before(before) {
		t.Fatalf("terminal entry ApprovedAt = %v, want no earlier than %v", tr.ApprovedAt, before)
	}
	if len(mockRun.ApplyTaskCalls) != 2 {
		t.Fatalf("expected 2 ApplyTaskResult calls, got %d", len(mockRun.ApplyTaskCalls))
	}
	running := mockRun.ApplyTaskCalls[0].TaskResult
	if running.ApprovedBy != "user:alice" || running.ApprovedAt == nil {
		t.Fatalf("initial running entry = %+v, want approver stamped", running)
	}
	terminal := mockRun.ApplyTaskCalls[1].TaskResult
	if terminal.ApprovedBy != "user:alice" || terminal.ApprovedAt == nil || !terminal.ApprovedAt.Equal(*running.ApprovedAt) {
		t.Fatalf("terminal entry = %+v, want same approver/time as running entry %+v", terminal, running)
	}
}

func TestRunService_RunTask_EmptyActorLeavesApprovalUnset(t *testing.T) {
	mockRun := &mocks.MockRunStore{ApplyTaskCh: make(chan struct{})}
	mockRunner := &mocks.MockPipelineRunner{
		TaskResultList: []domain.TaskResult{{Name: "manual", Status: "passed", Output: "ok"}},
	}
	svc := NewRunService(mockRun, mockRunner, mockRunner)

	tr, err := svc.RunTask(context.Background(), 7, "proj", &model.Project{Name: "proj", Tasks: []model.Task{{Name: "manual"}}}, "manual", domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.ApprovedBy != "" || tr.ApprovedAt != nil {
		t.Fatalf("terminal entry = %+v, want no approver when Actor is empty", tr)
	}
}

func TestRunService_ApproveManualTask_LogsActorOnSuccess(t *testing.T) {
	buf := captureLog(t)
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

	if _, err := svc.ApproveManualTask(context.Background(), 7, "proj", &model.Project{
		Name: "proj",
		Tasks: []model.Task{
			{Name: "build"},
			{Name: "manual", Manual: true, DependsOn: []string{"build"}},
		},
	}, "manual", domain.RunnerOptions{WorkDir: "/repo", Actor: "user:alice"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "manual task approved") || !strings.Contains(out, "account=user:alice") ||
		!strings.Contains(out, "project=proj") || !strings.Contains(out, "run_id=7") || !strings.Contains(out, "task=manual") {
		t.Fatalf("log output = %q, want an approved record naming account, project, run_id, and task", out)
	}
}

func TestRunService_ApproveManualTask_RefusalLogsNothingAndSkipsApply(t *testing.T) {
	buf := captureLog(t)
	mockRun := &mocks.MockRunStore{
		GetRunResult: &domain.Run{
			ID:          7,
			ProjectName: "proj",
			Status:      "running",
			PipelineDef: &model.Project{
				Name:  "proj",
				Tasks: []model.Task{{Name: "manual", Manual: true}},
			},
			TaskResults: []domain.TaskResult{{Name: "manual", Status: "passed"}},
		},
	}
	svc := NewRunService(mockRun, &emptyTaskRunner{}, &emptyTaskRunner{})

	_, err := svc.ApproveManualTask(context.Background(), 7, "proj", &model.Project{
		Name:  "proj",
		Tasks: []model.Task{{Name: "manual", Manual: true}},
	}, "manual", domain.RunnerOptions{Actor: "user:alice"})
	if err == nil {
		t.Fatal("expected refusal error because the task is not waiting for approval")
	}
	if len(mockRun.ApplyTaskCalls) != 0 {
		t.Fatalf("ApplyTaskResult should not be called on a refused approval, got %+v", mockRun.ApplyTaskCalls)
	}
	if strings.Contains(buf.String(), "run: approved") {
		t.Fatalf("log output = %q, want no approved record on a refused approval", buf.String())
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
	validate := func(*domain.ProjectConfig) (model.Project, error) { return model.Project{}, validationErr }

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
	runID, err := svc.createRun("proj", nil, false, &model.Project{Name: "proj"}, "")
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
	redactor := svc.prepareRunExecution(9, out, &opts)
	if opts.Stdout != redactor || opts.Stderr != redactor {
		t.Fatal("expected stdio to be wired to the redacting writer")
	}
	if _, err := opts.Stdout.Write([]byte("stdout line")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !strings.Contains(out.String(), "stdout line") {
		t.Fatalf("expected stdio to reach the underlying buffer, got %q", out.String())
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

// blockingPipelineRunner blocks Run until either releaseCh is closed or ctx
// is cancelled (a well-behaved executor honoring cancellation), signalling
// startedCh once it has begun so a test can synchronize on it.
type blockingPipelineRunner struct {
	startedCh   chan struct{}
	releaseCh   chan struct{}
	ignoreCtx   bool
	result      []domain.TaskResult
	resultErr   error
	startedOnce sync.Once
}

func (r *blockingPipelineRunner) Run(ctx context.Context, project domain.Project, changed map[string]bool, opts domain.RunnerOptions) ([]domain.TaskResult, error) {
	r.startedOnce.Do(func() { close(r.startedCh) })
	if r.ignoreCtx {
		<-r.releaseCh
	} else {
		select {
		case <-r.releaseCh:
		case <-ctx.Done():
		}
	}
	return r.result, r.resultErr
}

func (r *blockingPipelineRunner) RunTask(ctx context.Context, project domain.Project, taskName string, opts domain.RunnerOptions) (domain.TaskResult, error) {
	return domain.TaskResult{}, nil
}

// blockingTaskRunner blocks RunTask until either releaseCh is closed or ctx
// is cancelled (a well-behaved executor honoring cancellation), signalling
// startedCh once it has begun so a test can synchronize on it. It mirrors
// blockingPipelineRunner for the single-task path.
type blockingTaskRunner struct {
	startedCh   chan struct{}
	releaseCh   chan struct{}
	ignoreCtx   bool
	sawCancel   bool
	returnedCh  chan struct{}
	startedOnce sync.Once
}

func (r *blockingTaskRunner) Run(ctx context.Context, project domain.Project, changed map[string]bool, opts domain.RunnerOptions) ([]domain.TaskResult, error) {
	return nil, nil
}

func (r *blockingTaskRunner) RunTask(ctx context.Context, project domain.Project, taskName string, opts domain.RunnerOptions) (domain.TaskResult, error) {
	r.startedOnce.Do(func() { close(r.startedCh) })
	if r.ignoreCtx {
		<-r.releaseCh
	} else {
		select {
		case <-r.releaseCh:
		case <-ctx.Done():
		}
	}
	r.sawCancel = ctx.Err() != nil
	if r.returnedCh != nil {
		close(r.returnedCh)
	}
	// Mirrors runner.go's real Executor.RunTask: a task whose context was
	// canceled reports its own result as "canceled", not "passed" - see
	// REVIEW.md R1/R9.
	status := "passed"
	if r.sawCancel {
		status = "canceled"
	}
	return domain.TaskResult{Name: taskName, Status: status}, nil
}

func waitActiveAbsent(t *testing.T, svc *RunService, runID int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		svc.mu.Lock()
		_, stillActive := svc.active[runID]
		_, stillDone := svc.done[runID]
		svc.mu.Unlock()
		if !stillActive && !stillDone {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("run entry not removed from active/done maps after completion")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRunService_RunTask_RegistersActiveRunAndRemovesItOnCompletion(t *testing.T) {
	store := &mocks.MockRunStore{}
	runner := &blockingTaskRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{})}
	svc := NewRunService(store, runner, runner)

	const runID = 71
	retCh := make(chan error, 1)
	go func() {
		_, err := svc.RunTask(context.Background(), runID, "proj", &model.Project{Name: "proj"}, "t", domain.RunnerOptions{SourcesPrepared: true})
		retCh <- err
	}()

	select {
	case <-runner.startedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for task to start")
	}

	svc.mu.Lock()
	_, hasCancel := svc.active[runID]
	_, hasDone := svc.done[runID]
	svc.mu.Unlock()
	if !hasCancel || !hasDone {
		t.Fatal("run not registered in active/done maps while RunTask is executing")
	}

	close(runner.releaseCh)

	select {
	case err := <-retCh:
		if err != nil {
			t.Fatalf("RunTask: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for RunTask to return")
	}
	waitActiveAbsent(t, svc, runID)
}

func manualApprovalFixture(runID int) (*mocks.MockRunStore, *model.Project) {
	proj := &model.Project{
		Name: "proj",
		Tasks: []model.Task{
			{Name: "build"},
			{Name: "manual", Manual: true, DependsOn: []string{"build"}},
		},
	}
	store := &mocks.MockRunStore{
		GetRunResult: &domain.Run{
			ID:          runID,
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
	return store, proj
}

func TestRunService_ApproveManualTask_RegistersActiveRunAndRemovesItOnCompletion(t *testing.T) {
	const runID = 72
	store, proj := manualApprovalFixture(runID)
	runner := &blockingTaskRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{})}
	svc := NewRunService(store, runner, runner)

	retCh := make(chan error, 1)
	go func() {
		_, err := svc.ApproveManualTask(context.Background(), runID, "proj", proj, "manual", domain.RunnerOptions{SourcesPrepared: true})
		retCh <- err
	}()

	select {
	case <-runner.startedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for task to start")
	}

	svc.mu.Lock()
	_, hasCancel := svc.active[runID]
	_, hasDone := svc.done[runID]
	svc.mu.Unlock()
	if !hasCancel || !hasDone {
		t.Fatal("run not registered in active/done maps while ApproveManualTask is executing")
	}

	close(runner.releaseCh)

	select {
	case err := <-retCh:
		if err != nil {
			t.Fatalf("ApproveManualTask: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for ApproveManualTask to return")
	}
	waitActiveAbsent(t, svc, runID)
}

func TestRunService_ApproveManualTask_RejectsNilProjectAfterApproval(t *testing.T) {
	const runID = 74
	store, _ := manualApprovalFixture(runID)
	runner := &mocks.MockPipelineRunner{}
	svc := NewRunService(store, runner, runner)

	if _, err := svc.ApproveManualTask(context.Background(), runID, "proj", nil, "manual", domain.RunnerOptions{}); err == nil {
		t.Fatal("expected error for nil project")
	}
}

func TestRunService_ApproveManualTask_RejectsNilTaskRunner(t *testing.T) {
	const runID = 75
	store, proj := manualApprovalFixture(runID)
	svc := NewRunService(store, nil, nil)

	if _, err := svc.ApproveManualTask(context.Background(), runID, "proj", proj, "manual", domain.RunnerOptions{}); err == nil {
		t.Fatal("expected error for nil task runner")
	}
}

func TestRunService_TrackActive_ConcurrentCallsDoNotClobber(t *testing.T) {
	store := &mocks.MockRunStore{}
	svc := NewRunService(store, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})

	const runID = 76
	_, cleanupA := svc.trackActive(context.Background(), runID, "proj")
	runCtxB, cleanupB := svc.trackActive(context.Background(), runID, "proj")

	// The first call finishing must not remove the second, still-running
	// call's entry, or Cancel would miss it entirely.
	cleanupA()
	if runCtxB.Err() != nil {
		t.Fatal("first cleanup canceled the second call's context")
	}
	svc.mu.Lock()
	_, hasActive := svc.active[runID]
	_, hasDone := svc.done[runID]
	svc.mu.Unlock()
	if !hasActive || !hasDone {
		t.Fatal("first cleanup removed the still-running call's entry")
	}

	cancelErrCh := make(chan error, 1)
	go func() {
		cancelErrCh <- svc.Cancel(context.Background(), runID, "user:alice")
	}()
	// Cancel blocks on the second call's done channel until it returns.
	time.Sleep(50 * time.Millisecond)
	cleanupB()

	select {
	case err := <-cancelErrCh:
		if err != nil {
			t.Fatalf("Cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for Cancel to terminate the second call")
	}
	if runCtxB.Err() == nil {
		t.Fatal("Cancel did not terminate the second call's context")
	}
	if len(store.CancelRunCalls) != 1 {
		t.Fatalf("CancelRunCalls = %+v, want exactly 1", store.CancelRunCalls)
	}
	waitActiveAbsent(t, svc, runID)
}

func TestRunService_Cancel_TerminatesActiveRunTask(t *testing.T) {
	const runID = 73
	store := &mocks.MockRunStore{}
	runner := &blockingTaskRunner{
		startedCh:  make(chan struct{}),
		releaseCh:  make(chan struct{}),
		returnedCh: make(chan struct{}),
	}
	svc := NewRunService(store, runner, runner)

	runTaskErrCh := make(chan error, 1)
	go func() {
		_, err := svc.RunTask(context.Background(), runID, "proj", &model.Project{Name: "proj"}, "t", domain.RunnerOptions{SourcesPrepared: true})
		runTaskErrCh <- err
	}()

	select {
	case <-runner.startedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for task to start")
	}

	cancelErrCh := make(chan error, 1)
	go func() {
		cancelErrCh <- svc.Cancel(context.Background(), runID, "user:alice")
	}()

	select {
	case <-runner.returnedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for the blocking task to return after Cancel")
	}
	if !runner.sawCancel {
		t.Fatal("blocking taskRunner did not observe context cancellation after Cancel")
	}

	select {
	case err := <-runTaskErrCh:
		if err != nil {
			t.Fatalf("RunTask: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for RunTask to return")
	}

	select {
	case err := <-cancelErrCh:
		if err != nil {
			t.Fatalf("Cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for Cancel to return after the task returned")
	}

	if len(store.CancelRunCalls) != 1 || store.CancelRunCalls[0].ID != runID {
		t.Fatalf("CancelRunCalls = %+v, want exactly one call for run %d after the task returned", store.CancelRunCalls, runID)
	}
}

// TestRunService_Cancel_PersistsCanceledByWhenRunTaskLosesTheRace covers
// REVIEW.md R9: runTaskInternal's own ApplyTaskResult call - not CancelRun -
// performs the canceled transition here (CancelRun's status-guarded UPDATE,
// R2, loses the race and no-ops), so nothing would ever persist who
// canceled the run unless runTaskInternal does it itself via the actor
// Cancel recorded on the active entry before canceling its context.
func TestRunService_Cancel_PersistsCanceledByWhenRunTaskLosesTheRace(t *testing.T) {
	const runID = 74
	store := &mocks.MockRunStore{}
	runner := &blockingTaskRunner{
		startedCh:  make(chan struct{}),
		releaseCh:  make(chan struct{}),
		returnedCh: make(chan struct{}),
	}
	svc := NewRunService(store, runner, runner)

	runTaskErrCh := make(chan error, 1)
	go func() {
		_, err := svc.RunTask(context.Background(), runID, "proj", &model.Project{Name: "proj"}, "t", domain.RunnerOptions{SourcesPrepared: true})
		runTaskErrCh <- err
	}()

	select {
	case <-runner.startedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for task to start")
	}

	cancelErrCh := make(chan error, 1)
	go func() {
		cancelErrCh <- svc.Cancel(context.Background(), runID, "user:alice")
	}()

	select {
	case <-runner.returnedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for the blocking task to return after Cancel")
	}
	if err := <-runTaskErrCh; err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if err := <-cancelErrCh; err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if len(store.SetCanceledByCalls) != 1 || store.SetCanceledByCalls[0].ID != runID || store.SetCanceledByCalls[0].CanceledBy != "user:alice" {
		t.Fatalf("SetCanceledByCalls = %+v, want exactly one call for run %d by user:alice", store.SetCanceledByCalls, runID)
	}
	// CancelRun itself lost the race (the run is already canceled by the
	// time it runs) and must not also try to set canceled_by.
	if len(store.CancelRunCalls) != 1 {
		t.Fatalf("CancelRunCalls = %+v, want exactly 1", store.CancelRunCalls)
	}
}

func TestRunService_Start_RegistersActiveRunAndRemovesItOnCompletion(t *testing.T) {
	store := &mocks.MockRunStore{CreateRunID: 7}
	runner := &blockingPipelineRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{})}
	svc := NewRunService(store, runner, runner)

	runID, err := svc.Start(context.Background(), "proj", &model.Project{Name: "proj", Tasks: []model.Task{{Name: "t"}}}, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case <-runner.startedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for run to start")
	}

	svc.mu.Lock()
	_, hasCancel := svc.active[runID]
	_, hasDone := svc.done[runID]
	svc.mu.Unlock()
	if !hasCancel || !hasDone {
		t.Fatal("run not registered in active/done maps while running")
	}

	close(runner.releaseCh)

	deadline := time.Now().Add(3 * time.Second)
	for {
		svc.mu.Lock()
		_, stillActive := svc.active[runID]
		svc.mu.Unlock()
		if !stillActive {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("run entry not removed from active/done maps after completion")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRunService_Cancel_ActiveRunTerminatesInTime(t *testing.T) {
	store := &mocks.MockRunStore{CreateRunID: 8}
	runner := &blockingPipelineRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{})}
	svc := NewRunService(store, runner, runner)

	runID, err := svc.Start(context.Background(), "proj", &model.Project{Name: "proj", Tasks: []model.Task{{Name: "t"}}}, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-runner.startedCh

	if err := svc.Cancel(context.Background(), runID, "user:alice"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if len(store.CancelRunCalls) != 1 || store.CancelRunCalls[0].ID != runID {
		t.Fatalf("CancelRunCalls = %v, want [%d]", store.CancelRunCalls, runID)
	}
	if store.CancelRunCalls[0].CanceledBy != "user:alice" {
		t.Fatalf("CanceledBy = %q, want user:alice", store.CancelRunCalls[0].CanceledBy)
	}
}

// TestRunService_Cancel_PersistsCanceledByForMultiTaskRun covers REVIEW.md
// R9 on the Start/executeRun/finishRun path (not just RunTask/
// ApproveManualTask): when the pipeline itself reports a task as "canceled"
// (ctx canceled mid-execution), finishRun's own FinishRun call wins the
// race to the canceled transition before CancelRun runs, so it must persist
// canceled_by itself using the actor Cancel recorded on the active entry.
func TestRunService_Cancel_PersistsCanceledByForMultiTaskRun(t *testing.T) {
	store := &mocks.MockRunStore{CreateRunID: 9}
	runner := &blockingPipelineRunner{
		startedCh: make(chan struct{}),
		releaseCh: make(chan struct{}),
		result:    []domain.TaskResult{{Name: "t", Status: "canceled", Error: "canceled"}},
	}
	svc := NewRunService(store, runner, runner)

	runID, err := svc.Start(context.Background(), "proj", &model.Project{Name: "proj", Tasks: []model.Task{{Name: "t"}}}, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-runner.startedCh

	if err := svc.Cancel(context.Background(), runID, "user:bob"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if len(store.SetCanceledByCalls) != 1 || store.SetCanceledByCalls[0].ID != runID || store.SetCanceledByCalls[0].CanceledBy != "user:bob" {
		t.Fatalf("SetCanceledByCalls = %+v, want exactly one call for run %d by user:bob", store.SetCanceledByCalls, runID)
	}
}

// TestRunService_Start_CanceledTaskWithNoKnownActorSkipsSetCanceledBy covers
// persistCanceledByIfLost's defensive branch: a run can only ever reach a
// "canceled" task result through RunService.Cancel canceling its context, so
// this models the case where Cancel is never called on this RunService
// instance - the active entry then has no recorded canceledBy, and
// persistCanceledByIfLost must skip the store call rather than record an
// empty actor.
func TestRunService_Start_CanceledTaskWithNoKnownActorSkipsSetCanceledBy(t *testing.T) {
	store := &mocks.MockRunStore{CreateRunID: 10}
	runner := &mocks.MockPipelineRunner{
		TaskResultList: []domain.TaskResult{{Name: "t", Status: "canceled", Error: "canceled"}},
	}
	svc := NewRunService(store, runner, runner)

	runID, err := svc.Start(context.Background(), "proj", &model.Project{Name: "proj", Tasks: []model.Task{{Name: "t"}}}, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Wait for the run to fall out of active/done - the last thing
	// executeRun's goroutine does, strictly after finishRun (and any
	// persistCanceledByIfLost call within it) has returned.
	waitActiveAbsent(t, svc, runID)

	if len(store.SetCanceledByCalls) != 0 {
		t.Fatalf("SetCanceledByCalls = %+v, want none - Cancel was never called, so no actor is known", store.SetCanceledByCalls)
	}
}

// TestRunService_Cancel_DoneChBranch_SkipsMetricWhenAlreadyTerminal covers
// the double-count fix: once executeRun's own finishRun has already
// recorded a metric for a run that actually executed (its store status is
// already terminal by the time doneCh closes), CancelRun's own
// status-guarded UPDATE performs no transition - modeled here via
// CancelRunNoChange - so the doneCh branch must not record a second metric.
func TestRunService_Cancel_DoneChBranch_SkipsMetricWhenAlreadyTerminal(t *testing.T) {
	store := &mocks.MockRunStore{
		CreateRunID:       8,
		GetRunResult:      &domain.Run{ID: 8, ProjectName: "proj", Status: "passed"},
		CancelRunNoChange: true,
	}
	runner := &blockingPipelineRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{})}
	svc := NewRunService(store, runner, runner)
	rec := &fakeRecorder{}
	svc.Metrics = rec

	runID, err := svc.Start(context.Background(), "proj", &model.Project{Name: "proj", Tasks: []model.Task{{Name: "t"}}}, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-runner.startedCh

	if err := svc.Cancel(context.Background(), runID, "user:alice"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	// The async completion path (finishRun, via the blockingPipelineRunner
	// returning nil/nil on ctx-done) records exactly one "passed" entry;
	// Cancel's own check sees CancelRun reported no transition and correctly
	// records nothing further.
	runs := rec.snapshot()
	if len(runs) != 1 || runs[0].status != "passed" {
		t.Fatalf("recorded runs = %+v, want exactly the one entry from the async completion, none from Cancel", runs)
	}
}

// TestRunService_Cancel_DoneChBranch_RecordsMetricForQueuedRun covers the
// other half: a run canceled while still queued never reaches executeRun,
// so nothing else records it - the doneCh branch must record it itself. One
// shared blockingPipelineRunner occupies the only slot; the second run never
// calls its Run method at all while queued, so sharing it is race-free.
func TestRunService_Cancel_DoneChBranch_RecordsMetricForQueuedRun(t *testing.T) {
	blocker := &blockingPipelineRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{})}
	inner := &mocks.MockRunStore{
		GetRunResult: &domain.Run{ProjectName: "proj", Status: "queued"},
	}
	store := &seqIDMockRunStore{MockRunStore: inner}
	svc := NewRunService(store, blocker, blocker)
	svc.SetMaxConcurrentRuns(1)
	rec := &fakeRecorder{}
	svc.Metrics = rec

	_, err := svc.Start(context.Background(), "blocker", &model.Project{Name: "blocker", Tasks: []model.Task{{Name: "t"}}}, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start (blocker): %v", err)
	}
	<-blocker.startedCh
	defer close(blocker.releaseCh)

	runID, err := svc.Start(context.Background(), "proj", &model.Project{Name: "proj", Tasks: []model.Task{{Name: "t"}}}, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start (queued): %v", err)
	}
	deadline := time.After(3 * time.Second)
	for store.SetRunQueuedCallCount() == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for the second run to queue")
		case <-time.After(5 * time.Millisecond):
		}
	}

	if err := svc.Cancel(context.Background(), runID, "user:alice"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	runs := rec.snapshot()
	if len(runs) != 1 || runs[0].status != "canceled" || runs[0].project != "proj" {
		t.Fatalf("recorded runs = %+v, want exactly one canceled/proj entry", runs)
	}
}

func TestRunService_Cancel_ActiveRunStoreErrorReturnedUnchanged(t *testing.T) {
	wantErr := errors.New("store exploded")
	store := &mocks.MockRunStore{CreateRunID: 9, CancelRunErr: wantErr}
	runner := &blockingPipelineRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{})}
	svc := NewRunService(store, runner, runner)

	runID, err := svc.Start(context.Background(), "proj", &model.Project{Name: "proj", Tasks: []model.Task{{Name: "t"}}}, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-runner.startedCh

	err = svc.Cancel(context.Background(), runID, "user:alice")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Cancel err = %v, want %v", err, wantErr)
	}
}

func TestRunService_Cancel_TimesOutWhenRunDoesNotTerminate(t *testing.T) {
	oldBuffer := CancelWaitBuffer
	CancelWaitBuffer = 50 * time.Millisecond
	t.Cleanup(func() { CancelWaitBuffer = oldBuffer })

	store := &mocks.MockRunStore{CreateRunID: 9}
	runner := &blockingPipelineRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{}), ignoreCtx: true}
	svc := NewRunService(store, runner, runner)
	svc.CancelGrace = time.Millisecond

	runID, err := svc.Start(context.Background(), "proj", &model.Project{Name: "proj", Tasks: []model.Task{{Name: "t"}}}, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-runner.startedCh
	defer close(runner.releaseCh) // let the leaked goroutine exit after the test

	err = svc.Cancel(context.Background(), runID, "")
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if len(store.CancelRunCalls) != 0 {
		t.Fatalf("CancelRun should not be called on timeout, got %v", store.CancelRunCalls)
	}
}

func TestRunService_Cancel_NoActiveEntryCancelsInStoreDirectly(t *testing.T) {
	started := time.Now().Add(-time.Minute)
	store := &mocks.MockRunStore{
		GetRunResult: &domain.Run{ID: 123, ProjectName: "proj", Status: "queued", StartedAt: started},
	}
	svc := NewRunService(store, nil, nil)
	rec := &fakeRecorder{}
	svc.Metrics = rec

	if err := svc.Cancel(context.Background(), 123, "user:alice"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if len(store.CancelRunCalls) != 1 || store.CancelRunCalls[0].ID != 123 || store.CancelRunCalls[0].CanceledBy != "user:alice" {
		t.Fatalf("CancelRunCalls = %v, want [{123 user:alice}]", store.CancelRunCalls)
	}
	runs := rec.snapshot()
	if len(runs) != 1 || runs[0].status != "canceled" || runs[0].project != "proj" {
		t.Fatalf("recorded runs = %+v, want exactly one canceled/proj entry", runs)
	}
	if runs[0].duration < time.Minute {
		t.Errorf("duration = %v, want at least 1m (measured from GetRun's StartedAt)", runs[0].duration)
	}
}

// TestRunService_Cancel_NoActiveEntryGetRunFailsStillCancels covers the
// GetRun-error sub-branch: Cancel still succeeds (the store call it actually
// depends on is CancelRun, not this lookup), but records no metric -
// recordCanceledMetric's post-CancelRun GetRun call (for project/startedAt)
// fails, so it has nothing to ground a metric in and skips recording rather
// than guessing; see recordCanceledMetric.
func TestRunService_Cancel_NoActiveEntryGetRunFailsStillCancels(t *testing.T) {
	store := &mocks.MockRunStore{GetRunErr: errors.New("lookup failed")}
	svc := NewRunService(store, nil, nil)
	rec := &fakeRecorder{}
	svc.Metrics = rec

	if err := svc.Cancel(context.Background(), 123, "user:alice"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if len(store.CancelRunCalls) != 1 {
		t.Fatalf("CancelRunCalls = %v, want exactly 1 (Cancel must still proceed)", store.CancelRunCalls)
	}
	if runs := rec.snapshot(); len(runs) != 0 {
		t.Fatalf("recorded runs = %+v, want none when the pre-check can't confirm cancelability", runs)
	}
}

func TestRunService_Cancel_StoreErrorReturnedUnchanged(t *testing.T) {
	wantErr := errors.New("store exploded")
	store := &mocks.MockRunStore{CancelRunErr: wantErr}
	svc := NewRunService(store, nil, nil)

	err := svc.Cancel(context.Background(), 123, "user:alice")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Cancel err = %v, want %v", err, wantErr)
	}
}

func TestRunService_Cancel_CallerContextCancelledWhileWaiting(t *testing.T) {
	oldBuffer := CancelWaitBuffer
	CancelWaitBuffer = time.Minute
	t.Cleanup(func() { CancelWaitBuffer = oldBuffer })

	store := &mocks.MockRunStore{CreateRunID: 10}
	runner := &blockingPipelineRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{}), ignoreCtx: true}
	svc := NewRunService(store, runner, runner)
	svc.CancelGrace = time.Minute

	runID, err := svc.Start(context.Background(), "proj", &model.Project{Name: "proj", Tasks: []model.Task{{Name: "t"}}}, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-runner.startedCh
	defer close(runner.releaseCh) // let the leaked goroutine exit after the test

	callerCtx, cancelCaller := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancelCaller()
	}()

	err = svc.Cancel(callerCtx, runID, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Cancel err = %v, want context.Canceled", err)
	}
	if len(store.CancelRunCalls) != 0 {
		t.Fatalf("CancelRun should not be called when the caller's context is cancelled, got %v", store.CancelRunCalls)
	}
}

func TestRunService_Cancel_NoActiveEntryAndNoStore(t *testing.T) {
	svc := NewRunService(nil, nil, nil)
	if err := svc.Cancel(context.Background(), 1, ""); err == nil {
		t.Fatal("expected an error when runs are not supported")
	}
}

func TestWebhookService_Record_NilReceiverAndSink(t *testing.T) {
	var nilSvc *WebhookService
	nilSvc.Record(domain.WebhookEvent{Repo: "a"})

	sink := &mocks.MockWebhookLogSink{}
	svc := NewWebhookService(sink)
	svc.Record(domain.WebhookEvent{Repo: "org/service-a", Status: domain.WebhookStatusTriggered})
	if len(sink.Entries) != 1 {
		t.Fatalf("expected 1 recorded event, got %d", len(sink.Entries))
	}
}

// seqRunStore returns incrementing run IDs so two concurrent Starts occupy
// distinct active entries. Only CreateRun/FinishRun/AppendTaskResult matter
// for Start's lifecycle; the rest are no-ops satisfying domain.RunStore.
type seqRunStore struct {
	mu     sync.Mutex
	next   int
	finish int
}

func (s *seqRunStore) CreateRun(string, []string, bool, *model.Project, string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	return 100 + s.next, nil
}

func (s *seqRunStore) FinishRun(int, string, string, string, []domain.TaskResult) error {
	s.mu.Lock()
	s.finish++
	s.mu.Unlock()
	return nil
}

func (s *seqRunStore) AppendTaskResult(int, domain.TaskResult) error { return nil }
func (s *seqRunStore) FlushRunOutput(int, string) error              { return nil }
func (s *seqRunStore) CancelRun(int, string) (bool, error)           { return true, nil }
func (s *seqRunStore) SetCanceledBy(int, string) error               { return nil }
func (s *seqRunStore) ApplyTaskResult(int, domain.TaskResult, string) (string, error) {
	return "", nil
}
func (s *seqRunStore) GetRun(int) (*domain.Run, error)                 { return nil, nil }
func (s *seqRunStore) CountRuns(string) (int, error)                   { return 0, nil }
func (s *seqRunStore) ListRuns(string, int, int) ([]domain.Run, error) { return nil, nil }
func (s *seqRunStore) ListAllRuns(int) ([]domain.Run, error)           { return nil, nil }
func (s *seqRunStore) CountRunsByStatus() (map[string]int, error)      { return nil, nil }
func (s *seqRunStore) FailInterruptedRuns(string) (int, error)         { return 0, nil }
func (s *seqRunStore) SetRunQueued(int) error                          { return nil }
func (s *seqRunStore) StartQueuedRun(int) (bool, error)                { return true, nil }

// twoSlotBlockRunner blocks each Run call on its own release channel so a
// test can complete one run while the other stays active. started is
// buffered for the two expected calls.
type twoSlotBlockRunner struct {
	mu       sync.Mutex
	calls    int
	started  chan struct{}
	release1 chan struct{}
	release2 chan struct{}
}

func (r *twoSlotBlockRunner) Run(ctx context.Context, _ domain.Project, _ map[string]bool, _ domain.RunnerOptions) ([]domain.TaskResult, error) {
	r.mu.Lock()
	idx := r.calls
	r.calls++
	r.mu.Unlock()
	r.started <- struct{}{}
	if idx == 0 {
		select {
		case <-r.release1:
		case <-ctx.Done():
		}
	} else {
		select {
		case <-r.release2:
		case <-ctx.Done():
		}
	}
	return []domain.TaskResult{{Name: "t", Status: "passed"}}, nil
}

func (r *twoSlotBlockRunner) RunTask(context.Context, domain.Project, string, domain.RunnerOptions) (domain.TaskResult, error) {
	return domain.TaskResult{}, nil
}

func TestRunService_ActiveRunIDsForProject_TracksConcurrentRuns(t *testing.T) {
	store := &seqRunStore{}
	runner := &twoSlotBlockRunner{
		started:  make(chan struct{}, 2),
		release1: make(chan struct{}),
		release2: make(chan struct{}),
	}
	svc := NewRunService(store, runner, runner)
	proj := &model.Project{Name: "proj", Tasks: []model.Task{{Name: "t"}}}

	id1, err := svc.Start(context.Background(), "proj", proj, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start 1: %v", err)
	}
	id2, err := svc.Start(context.Background(), "proj", proj, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start 2: %v", err)
	}
	if id1 == id2 {
		t.Fatalf("expected distinct run IDs, got %d twice", id1)
	}

	for i := 0; i < 2; i++ {
		select {
		case <-runner.started:
		case <-time.After(3 * time.Second):
			t.Fatal("timeout waiting for both runs to start")
		}
	}

	got := svc.ActiveRunIDsForProject("proj")
	if len(got) != 2 {
		t.Fatalf("ActiveRunIDsForProject(proj) = %v, want 2 entries", got)
	}
	if got[0] != min(id1, id2) || got[1] != max(id1, id2) {
		t.Fatalf("ActiveRunIDsForProject(proj) = %v, want %d and %d in ascending order", got, id1, id2)
	}
	if forOther := svc.ActiveRunIDsForProject("other"); len(forOther) != 0 {
		t.Fatalf("ActiveRunIDsForProject(other) = %v, want empty", forOther)
	}

	close(runner.release1)
	deadline := time.Now().Add(3 * time.Second)
	for {
		got = svc.ActiveRunIDsForProject("proj")
		if len(got) == 1 && (got[0] == id1 || got[0] == id2) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after completing one run, ActiveRunIDsForProject(proj) = %v, want exactly one of [%d %d]", got, id1, id2)
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(runner.release2)
	deadline = time.Now().Add(3 * time.Second)
	for {
		if len(svc.ActiveRunIDsForProject("proj")) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after completing both runs, ActiveRunIDsForProject(proj) = %v, want empty", svc.ActiveRunIDsForProject("proj"))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestRunService_ActiveRunIDsForProject_TracksSingleTaskRun covers the
// second registration site: RunTask records the run through trackActive,
// not through Start, and the project it records is what lets the delete
// guard refuse a project whose single-task run is still executing.
func TestRunService_ActiveRunIDsForProject_TracksSingleTaskRun(t *testing.T) {
	runner := &blockingTaskRunner{
		startedCh: make(chan struct{}),
		releaseCh: make(chan struct{}),
	}
	svc := NewRunService(&mocks.MockRunStore{}, runner, runner)

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		svc.RunTask(context.Background(), 7, "proj", &model.Project{
			Name:  "proj",
			Tasks: []model.Task{{Name: "manual"}},
		}, "manual", domain.RunnerOptions{})
	}()

	select {
	case <-runner.startedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for the task to start")
	}

	if got := svc.ActiveRunIDsForProject("proj"); len(got) != 1 || got[0] != 7 {
		t.Fatalf("ActiveRunIDsForProject(proj) = %v, want [7]", got)
	}
	if got := svc.ActiveRunIDsForProject("other"); len(got) != 0 {
		t.Fatalf("ActiveRunIDsForProject(other) = %v, want empty", got)
	}

	close(runner.releaseCh)
	select {
	case <-doneCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for RunTask to return")
	}
	if got := svc.ActiveRunIDsForProject("proj"); len(got) != 0 {
		t.Fatalf("after the task returned, ActiveRunIDsForProject(proj) = %v, want empty", got)
	}
}

// seqIDMockRunStore hands out increasing run IDs so concurrent Starts occupy
// distinct entries in RunService.active/done, while still recording every
// call (including SetRunQueued/StartQueuedRun) via the embedded MockRunStore.
type seqIDMockRunStore struct {
	*mocks.MockRunStore
	mu   sync.Mutex
	next int
}

func (s *seqIDMockRunStore) CreateRun(p string, c []string, im bool, snap *model.Project, startedBy string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	return s.next, nil
}

// concurrencyBlockRunner blocks every Run call until release is closed, and
// signals each call's start on started (buffered so a test can count
// concurrent starts without a race).
type concurrencyBlockRunner struct {
	started chan struct{}
	release chan struct{}
}

func (r *concurrencyBlockRunner) Run(ctx context.Context, _ domain.Project, _ map[string]bool, _ domain.RunnerOptions) ([]domain.TaskResult, error) {
	r.started <- struct{}{}
	select {
	case <-r.release:
	case <-ctx.Done():
	}
	return []domain.TaskResult{{Name: "t", Status: "passed"}}, nil
}

func (r *concurrencyBlockRunner) RunTask(context.Context, domain.Project, string, domain.RunnerOptions) (domain.TaskResult, error) {
	return domain.TaskResult{}, nil
}

// TestRunService_Start_ConcurrencyLimit_QueuesBeyondCapacity covers the spec
// scenario "Burst above the limit": at limit 2, three starts yield two
// running and one queued, and the queue write happens synchronously (before
// Start returns) so a caller polling immediately never races the
// background goroutine's own scheduling.
func TestRunService_Start_ConcurrencyLimit_QueuesBeyondCapacity(t *testing.T) {
	store := &seqIDMockRunStore{MockRunStore: &mocks.MockRunStore{StartQueuedRunResult: true}}
	runner := &concurrencyBlockRunner{started: make(chan struct{}, 3), release: make(chan struct{})}
	svc := NewRunService(store, runner, runner)
	svc.SetMaxConcurrentRuns(2)

	proj := &model.Project{Name: "p", Tasks: []model.Task{{Name: "t"}}}
	runIDs := make([]int, 3)
	for i := 0; i < 3; i++ {
		id, err := svc.Start(context.Background(), "p", proj, nil, domain.RunnerOptions{})
		if err != nil {
			t.Fatalf("Start #%d: %v", i, err)
		}
		runIDs[i] = id
	}

	// Synchronous by construction: no race to wait out.
	if len(store.SetRunQueuedCalls) != 1 || store.SetRunQueuedCalls[0] != runIDs[2] {
		t.Fatalf("SetRunQueuedCalls = %v, want exactly [%d] (third run queued)", store.SetRunQueuedCalls, runIDs[2])
	}

	for i := 0; i < 2; i++ {
		select {
		case <-runner.started:
		case <-time.After(3 * time.Second):
			t.Fatalf("timeout waiting for run #%d to start executing", i)
		}
	}
	select {
	case <-runner.started:
		t.Fatal("a third run executed despite the concurrency limit")
	case <-time.After(100 * time.Millisecond):
	}

	close(runner.release)
	select {
	case <-runner.started:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for the queued run to start after a slot freed")
	}
	if len(store.StartQueuedRunCalls) != 1 || store.StartQueuedRunCalls[0] != runIDs[2] {
		t.Fatalf("StartQueuedRunCalls = %v, want exactly [%d]", store.StartQueuedRunCalls, runIDs[2])
	}
}

// TestRunService_Cancel_QueuedRun_PromotesNextWaiter covers the spec
// scenario "Cancel while queued": canceling a queued run never executes it,
// and the slot it was waiting for goes to the next queued run instead.
func TestRunService_Cancel_QueuedRun_PromotesNextWaiter(t *testing.T) {
	store := &seqIDMockRunStore{MockRunStore: &mocks.MockRunStore{StartQueuedRunResult: true}}
	runner := &concurrencyBlockRunner{started: make(chan struct{}, 3), release: make(chan struct{})}
	svc := NewRunService(store, runner, runner)
	svc.SetMaxConcurrentRuns(1)
	proj := &model.Project{Name: "p", Tasks: []model.Task{{Name: "t"}}}

	_, err := svc.Start(context.Background(), "p", proj, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start a: %v", err)
	}
	select {
	case <-runner.started:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for run a to start")
	}

	bID, err := svc.Start(context.Background(), "p", proj, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start b: %v", err)
	}
	cID, err := svc.Start(context.Background(), "p", proj, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start c: %v", err)
	}
	if len(store.SetRunQueuedCalls) != 2 {
		t.Fatalf("SetRunQueuedCalls = %v, want b and c both queued", store.SetRunQueuedCalls)
	}

	if err := svc.Cancel(context.Background(), bID, "user:alice"); err != nil {
		t.Fatalf("Cancel queued run b: %v", err)
	}
	if len(store.CancelRunCalls) != 1 || store.CancelRunCalls[0].ID != bID {
		t.Fatalf("CancelRunCalls = %+v, want exactly run b (%d)", store.CancelRunCalls, bID)
	}

	// b must never reach the pipeline runner while a still holds the only slot.
	select {
	case <-runner.started:
		t.Fatal("canceled queued run b executed despite never holding a slot")
	case <-time.After(100 * time.Millisecond):
	}

	close(runner.release) // frees a's slot
	select {
	case <-runner.started:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for the next waiter to start after the slot freed")
	}

	if len(store.StartQueuedRunCalls) != 1 || store.StartQueuedRunCalls[0] != cID {
		t.Fatalf("StartQueuedRunCalls = %v, want exactly c (%d) promoted, not the canceled b", store.StartQueuedRunCalls, cID)
	}
}

// TestRunService_RunTask_WaitsForConcurrencySlot covers the proposal's
// "single-task re-runs ... take a slot like any run": at limit 1, RunTask
// blocks until the occupying run releases its slot.
func TestRunService_RunTask_WaitsForConcurrencySlot(t *testing.T) {
	store := &mocks.MockRunStore{StartQueuedRunResult: true}
	pipelineRunner := &blockingPipelineRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{})}
	taskRunner := &blockingTaskRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{}), returnedCh: make(chan struct{})}
	svc := NewRunService(store, pipelineRunner, taskRunner)
	svc.SetMaxConcurrentRuns(1)

	_, err := svc.Start(context.Background(), "p", &model.Project{Name: "p", Tasks: []model.Task{{Name: "t"}}}, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-pipelineRunner.startedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for the pipeline run to occupy the only slot")
	}

	runTaskDone := make(chan struct{})
	go func() {
		defer close(runTaskDone)
		svc.RunTask(context.Background(), 99, "p", &model.Project{Name: "p"}, "single", domain.RunnerOptions{SourcesPrepared: true})
	}()

	select {
	case <-taskRunner.startedCh:
		t.Fatal("RunTask executed the task despite the concurrency limit")
	case <-time.After(100 * time.Millisecond):
	}

	close(pipelineRunner.releaseCh) // frees the only slot
	select {
	case <-taskRunner.startedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for RunTask to start once the slot freed")
	}
	close(taskRunner.releaseCh)

	select {
	case <-runTaskDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for RunTask to return")
	}
}

func TestRunService_SetMaxConcurrentRuns_ZeroRestoresUnlimited(t *testing.T) {
	svc := NewRunService(&mocks.MockRunStore{}, nil, nil)
	svc.SetMaxConcurrentRuns(1)
	if !svc.tryAcquireSlot() {
		t.Fatal("expected the single slot to be free")
	}
	svc.SetMaxConcurrentRuns(0)
	if !svc.tryAcquireSlot() {
		t.Fatal("SetMaxConcurrentRuns(0) should restore unlimited concurrency")
	}
	if !svc.tryAcquireSlot() {
		t.Fatal("a second acquire should also succeed under unlimited concurrency")
	}
}

func TestRunService_MaxConcurrentRuns_ReportsConfiguredCapacity(t *testing.T) {
	svc := NewRunService(&mocks.MockRunStore{}, nil, nil)
	if got := svc.MaxConcurrentRuns(); got != 0 {
		t.Errorf("MaxConcurrentRuns() = %d, want 0 (unlimited) before any SetMaxConcurrentRuns call", got)
	}
	svc.SetMaxConcurrentRuns(5)
	if got := svc.MaxConcurrentRuns(); got != 5 {
		t.Errorf("MaxConcurrentRuns() = %d, want 5", got)
	}
	svc.SetMaxConcurrentRuns(0)
	if got := svc.MaxConcurrentRuns(); got != 0 {
		t.Errorf("MaxConcurrentRuns() = %d, want 0 after restoring unlimited", got)
	}
}

func TestRunService_WaitForSlot_UnlimitedAlwaysSucceedsImmediately(t *testing.T) {
	svc := NewRunService(&mocks.MockRunStore{}, nil, nil)
	if !svc.waitForSlot(context.Background()) {
		t.Fatal("waitForSlot on an unlimited service should always succeed")
	}
}

// REVIEW.md R2: when a slot frees at the same instant ctx is canceled, Go's
// select can pick either ready case. Run many trials with a pre-canceled ctx
// and a single available token so both branches fire across the run; every
// trial must both report failure and leave the token available afterward -
// proving the token-received branch releases it back rather than letting the
// caller proceed on a dead context.
func TestRunService_WaitForSlot_NeverSucceedsWithAlreadyDoneContext(t *testing.T) {
	svc := NewRunService(&mocks.MockRunStore{}, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 200; i++ {
		svc.SetMaxConcurrentRuns(1) // fresh single-token channel each trial
		if svc.waitForSlot(ctx) {
			t.Fatal("waitForSlot must never succeed once ctx is already done, even when a token happens to be available")
		}
		if !svc.tryAcquireSlot() {
			t.Fatal("token was lost: waitForSlot must release it back when it wins the race against an already-done ctx")
		}
	}
}

func TestRunService_ReleaseSlot_UnexpectedOverflowIsNoop(t *testing.T) {
	svc := NewRunService(&mocks.MockRunStore{}, nil, nil)
	svc.SetMaxConcurrentRuns(1)
	// The single token is already in the channel (never acquired); releasing
	// again must not block or panic.
	svc.releaseSlot()
	if !svc.tryAcquireSlot() {
		t.Fatal("expected the one legitimate token to still be acquirable")
	}
}

func TestRunService_RunTask_CanceledWhileQueued(t *testing.T) {
	store := &mocks.MockRunStore{StartQueuedRunResult: true}
	pipelineRunner := &blockingPipelineRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{})}
	taskRunner := &blockingTaskRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{}), returnedCh: make(chan struct{})}
	svc := NewRunService(store, pipelineRunner, taskRunner)
	svc.SetMaxConcurrentRuns(1)

	if _, err := svc.Start(context.Background(), "p", &model.Project{Name: "p", Tasks: []model.Task{{Name: "t"}}}, nil, domain.RunnerOptions{}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-pipelineRunner.startedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for the pipeline run to occupy the only slot")
	}

	const runID = 55
	runTaskErrCh := make(chan error, 1)
	go func() {
		_, err := svc.RunTask(context.Background(), runID, "p", &model.Project{Name: "p"}, "single", domain.RunnerOptions{SourcesPrepared: true})
		runTaskErrCh <- err
	}()

	// Wait until RunTask is registered (so Cancel can find it), then cancel it
	// while it is still queued.
	waitActivePresent(t, svc, runID)
	if err := svc.Cancel(context.Background(), runID, "user:alice"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	select {
	case err := <-runTaskErrCh:
		if err == nil {
			t.Fatal("expected RunTask to report the run was canceled while queued")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for RunTask to return after Cancel")
	}
	select {
	case <-taskRunner.startedCh:
		t.Fatal("RunTask must not have executed the task after being canceled while queued")
	default:
	}
}

func TestRunService_ApproveManualTask_CanceledWhileQueued(t *testing.T) {
	const runID = 77
	store, proj := manualApprovalFixture(runID)
	pipelineRunner := &blockingPipelineRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{})}
	taskRunner := &blockingTaskRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{}), returnedCh: make(chan struct{})}
	svc := NewRunService(store, pipelineRunner, taskRunner)
	svc.SetMaxConcurrentRuns(1)

	if _, err := svc.Start(context.Background(), "other", &model.Project{Name: "other", Tasks: []model.Task{{Name: "t"}}}, nil, domain.RunnerOptions{}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-pipelineRunner.startedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for the pipeline run to occupy the only slot")
	}

	approveErrCh := make(chan error, 1)
	go func() {
		_, err := svc.ApproveManualTask(context.Background(), runID, "proj", proj, "manual", domain.RunnerOptions{SourcesPrepared: true})
		approveErrCh <- err
	}()

	waitActivePresent(t, svc, runID)
	if err := svc.Cancel(context.Background(), runID, "user:alice"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	select {
	case err := <-approveErrCh:
		if err == nil {
			t.Fatal("expected ApproveManualTask to report the run was canceled while queued")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for ApproveManualTask to return after Cancel")
	}
	select {
	case <-taskRunner.startedCh:
		t.Fatal("ApproveManualTask must not have executed the task after being canceled while queued")
	default:
	}
}

func TestRunService_RunTask_LosesQueuedRaceAfterSlotFrees(t *testing.T) {
	store := &mocks.MockRunStore{StartQueuedRunResult: false}
	pipelineRunner := &blockingPipelineRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{})}
	taskRunner := &blockingTaskRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{}), returnedCh: make(chan struct{})}
	svc := NewRunService(store, pipelineRunner, taskRunner)
	svc.SetMaxConcurrentRuns(1)

	if _, err := svc.Start(context.Background(), "p", &model.Project{Name: "p", Tasks: []model.Task{{Name: "t"}}}, nil, domain.RunnerOptions{}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-pipelineRunner.startedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for the pipeline run to occupy the only slot")
	}

	runTaskErrCh := make(chan error, 1)
	go func() {
		_, err := svc.RunTask(context.Background(), 56, "p", &model.Project{Name: "p"}, "single", domain.RunnerOptions{SourcesPrepared: true})
		runTaskErrCh <- err
	}()

	// Wait until RunTask has actually reached the queued-and-waiting state
	// before freeing the slot, or it could race tryAcquireSlot and grab the
	// token directly without ever queueing.
	deadline := time.After(3 * time.Second)
	for store.SetRunQueuedCallCount() == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for RunTask to queue")
		case <-time.After(5 * time.Millisecond):
		}
	}
	close(pipelineRunner.releaseCh) // frees the only slot; RunTask acquires the token but loses StartQueuedRun

	select {
	case err := <-runTaskErrCh:
		if err == nil {
			t.Fatal("expected an error when StartQueuedRun reports the run was already canceled")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for RunTask to return")
	}
	select {
	case <-taskRunner.startedCh:
		t.Fatal("RunTask must not execute after losing the queued race")
	default:
	}
	// The slot must have been given back for the next waiter.
	if !svc.tryAcquireSlot() {
		t.Fatal("expected the slot to be released back to the pool")
	}
}

func TestRunService_Start_LosesQueuedRaceAfterSlotFrees(t *testing.T) {
	store := &seqIDMockRunStore{MockRunStore: &mocks.MockRunStore{StartQueuedRunResult: false}}
	runner := &concurrencyBlockRunner{started: make(chan struct{}, 2), release: make(chan struct{})}
	svc := NewRunService(store, runner, runner)
	svc.SetMaxConcurrentRuns(1)
	proj := &model.Project{Name: "p", Tasks: []model.Task{{Name: "t"}}}

	if _, err := svc.Start(context.Background(), "p", proj, nil, domain.RunnerOptions{}); err != nil {
		t.Fatalf("Start a: %v", err)
	}
	select {
	case <-runner.started:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for run a to start")
	}

	if _, err := svc.Start(context.Background(), "p", proj, nil, domain.RunnerOptions{}); err != nil {
		t.Fatalf("Start b: %v", err)
	}

	close(runner.release) // frees a's slot; b acquires the token but loses StartQueuedRun

	select {
	case <-runner.started:
		t.Fatal("b must not execute after losing the queued race")
	case <-time.After(300 * time.Millisecond):
	}
	if !svc.tryAcquireSlot() {
		t.Fatal("expected the slot to be released back to the pool after the lost race")
	}
}

// waitActivePresent blocks until runID is registered in svc.active, or fails
// the test after a timeout.
func waitActivePresent(t *testing.T, svc *RunService, runID int) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		svc.mu.Lock()
		_, ok := svc.active[runID]
		svc.mu.Unlock()
		if ok {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for run %d to register in active", runID)
		case <-time.After(5 * time.Millisecond):
		}
	}
}
