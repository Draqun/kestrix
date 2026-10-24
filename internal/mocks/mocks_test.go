package mocks

import (
	"context"
	"errors"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/model"
)

func TestMockProjectStore_UpsertRecordsCall(t *testing.T) {
	m := &MockProjectStore{}
	cfg := &domain.ProjectConfig{Name: "demo", RepoURL: "https://git.example/demo.git", RepoRef: "main"}
	if err := m.Upsert(cfg); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if len(m.UpsertCalls) != 1 || m.UpsertCalls[0].Name != "demo" {
		t.Errorf("UpsertCalls = %+v, want one entry with Name=demo", m.UpsertCalls)
	}
}

func TestMockProjectStore_GetReturnsConfiguredError(t *testing.T) {
	m := &MockProjectStore{GetErr: errNotFound}
	_, err := m.Get("ghost")
	if err != errNotFound {
		t.Errorf("err = %v, want errNotFound", err)
	}
}

func TestMockProjectStore_DeleteRecordsCall(t *testing.T) {
	m := &MockProjectStore{}
	_ = m.Delete("p")
	if len(m.DeleteCalls) != 1 || m.DeleteCalls[0] != "p" {
		t.Errorf("DeleteCalls = %v, want [p]", m.DeleteCalls)
	}
}

func TestMockRunStore_CreateRunRecordsCall(t *testing.T) {
	m := &MockRunStore{CreateRunID: 42}
	snap := &model.Project{Name: "demo"}
	id, err := m.CreateRun("demo", []string{"svc"}, "", true, snap, "user:alice")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}
	if len(m.CreateRunCalls) != 1 || m.CreateRunCalls[0].ProjectName != "demo" {
		t.Errorf("CreateRunCalls = %+v", m.CreateRunCalls)
	}
}

func TestMockRunStore_FinishRunRecordsCall(t *testing.T) {
	m := &MockRunStore{}
	trs := []domain.TaskResult{{Name: "build", Status: "passed"}}
	_ = m.FinishRun(1, "passed", "out", "", trs)
	if len(m.FinishRunCalls) != 1 || m.FinishRunCalls[0].Status != "passed" {
		t.Errorf("FinishRunCalls = %+v", m.FinishRunCalls)
	}
}

func TestMockPipelineRunner_ReturnsConfiguredResults(t *testing.T) {
	m := &MockPipelineRunner{TaskResultList: []domain.TaskResult{{Name: "t", Status: "passed"}}}
	results, err := m.Run(context.Background(), model.Project{}, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 || results[0].Name != "t" {
		t.Errorf("results = %+v", results)
	}
}

func TestMockPipelineRunner_Panics(t *testing.T) {
	m := &MockPipelineRunner{PanicMsg: "boom"}
	defer func() {
		if r := recover(); r != "boom" {
			t.Errorf("recover = %v, want boom", r)
		}
	}()
	_, _ = m.Run(context.Background(), model.Project{}, nil, domain.RunnerOptions{})
}

func TestMockPipelineRunner_RunTaskPanics(t *testing.T) {
	m := &MockPipelineRunner{PanicMsg: "boom"}
	defer func() {
		if r := recover(); r != "boom" {
			t.Errorf("recover = %v, want boom", r)
		}
	}()
	_, _ = m.RunTask(context.Background(), model.Project{}, "lint", domain.RunnerOptions{})
}

func TestMockWebhookLogSink_RecordAndList(t *testing.T) {
	m := &MockWebhookLogSink{}
	if err := m.Record(domain.WebhookEvent{Repo: "push", Status: domain.WebhookStatusTriggered}); err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if err := m.Record(domain.WebhookEvent{Repo: "merge", Status: domain.WebhookStatusSkipped}); err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	list := m.List()
	if len(list) != 2 || list[0].Repo != "push" || list[1].Repo != "merge" {
		t.Errorf("List = %+v", list)
	}
	if list[0].ReceivedAt.IsZero() {
		t.Error("Record did not stamp ReceivedAt")
	}
}

func TestMockWebhookLogSink_RecordError(t *testing.T) {
	m := &MockWebhookLogSink{RecordErr: errors.New("boom")}
	if err := m.Record(domain.WebhookEvent{}); err == nil {
		t.Fatal("expected RecordErr to be returned")
	}
	if len(m.Entries) != 0 {
		t.Errorf("failed Record stored %d entries, want 0", len(m.Entries))
	}
}

func TestMockWebhookLogSink_FilterAndPaginate(t *testing.T) {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	m := &MockWebhookLogSink{}
	for i, e := range []domain.WebhookEvent{
		{ProjectName: "web", Status: domain.WebhookStatusTriggered},
		{ProjectName: "api", Status: domain.WebhookStatusError},
		{ProjectName: "web", Status: domain.WebhookStatusError},
	} {
		e.ReceivedAt = base.Add(time.Duration(i) * time.Minute)
		if err := m.Record(e); err != nil {
			t.Fatalf("Record returned error: %v", err)
		}
	}

	filter := domain.WebhookEventFilter{ProjectName: "web", Status: domain.WebhookStatusError}
	count, err := m.CountWebhookEvents(filter)
	if err != nil {
		t.Fatalf("CountWebhookEvents returned error: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}

	all, err := m.ListWebhookEvents(domain.WebhookEventFilter{}, 2, 0)
	if err != nil {
		t.Fatalf("ListWebhookEvents returned error: %v", err)
	}
	if len(all) != 2 || all[0].ProjectName != "web" || all[0].Status != domain.WebhookStatusError {
		t.Errorf("newest-first page = %+v", all)
	}

	since := base.Add(time.Minute)
	windowed, err := m.ListWebhookEvents(domain.WebhookEventFilter{Since: &since}, 10, 0)
	if err != nil {
		t.Fatalf("ListWebhookEvents returned error: %v", err)
	}
	if len(windowed) != 2 {
		t.Errorf("since filter returned %d events, want 2", len(windowed))
	}

	if page, err := m.ListWebhookEvents(domain.WebhookEventFilter{}, 10, 99); err != nil || page != nil {
		t.Errorf("offset past end = %+v, %v", page, err)
	}
}

func TestMockProjectStore_Load(t *testing.T) {
	m := &MockProjectStore{LoadResult: map[string]*domain.ProjectConfig{"p": {Name: "p"}}}
	r, err := m.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 1 {
		t.Errorf("Load returned %d items, want 1", len(r))
	}
}

func TestMockProjectStore_Save(t *testing.T) {
	m := &MockProjectStore{}
	if err := m.Save(nil); err != nil {
		t.Fatal(err)
	}
}

func TestMockProjectStore_FindByGitLabID(t *testing.T) {
	m := &MockProjectStore{}
	p, err := m.FindByGitLabID(42)
	if err != nil || p != nil {
		t.Errorf("FindByGitLabID = %v, %v", p, err)
	}
}

func TestMockProjectStore_List(t *testing.T) {
	m := &MockProjectStore{ListResult: []string{"a", "b"}}
	names, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Errorf("List returned %d items, want 2", len(names))
	}
}

func TestMockRunStore_AppendTaskResult(t *testing.T) {
	m := &MockRunStore{}
	tr := domain.TaskResult{Name: "build", Status: "passed"}
	if err := m.AppendTaskResult(1, tr); err != nil {
		t.Fatal(err)
	}
	if len(m.AppendTaskCalls) != 1 || m.AppendTaskCalls[0].Name != "build" {
		t.Errorf("AppendTaskCalls = %+v", m.AppendTaskCalls)
	}
}

func TestMockRunStore_FlushRunOutput(t *testing.T) {
	m := &MockRunStore{}
	if err := m.FlushRunOutput(1, "output"); err != nil {
		t.Fatal(err)
	}
	if len(m.FlushRunCalls) != 1 || m.FlushRunCalls[0] != "output" {
		t.Errorf("FlushRunCalls = %+v", m.FlushRunCalls)
	}
}

func TestMockRunStore_CancelRun(t *testing.T) {
	m := &MockRunStore{}
	changed, err := m.CancelRun(1, "user:alice")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("changed = false, want true by default")
	}
	if len(m.CancelRunCalls) != 1 || m.CancelRunCalls[0].ID != 1 || m.CancelRunCalls[0].CanceledBy != "user:alice" {
		t.Errorf("CancelRunCalls = %+v", m.CancelRunCalls)
	}
}

func TestMockRunStore_ApplyTaskResult(t *testing.T) {
	m := &MockRunStore{}
	tr := domain.TaskResult{Name: "deploy", Status: "running"}
	status, err := m.ApplyTaskResult(1, tr, "extra")
	if err != nil {
		t.Fatal(err)
	}
	if status != "running" {
		t.Errorf("status = %q, want running", status)
	}
	if len(m.ApplyTaskCalls) != 1 || m.ApplyTaskCalls[0].TaskResult.Name != "deploy" {
		t.Errorf("ApplyTaskCalls = %+v", m.ApplyTaskCalls)
	}
}

func TestMockRunStore_GetRun(t *testing.T) {
	m := &MockRunStore{GetRunResult: &domain.Run{ID: 1}}
	r, err := m.GetRun(1)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.ID != 1 {
		t.Errorf("GetRun = %+v", r)
	}
}

func TestMockRunStore_CountRuns(t *testing.T) {
	m := &MockRunStore{CountRunsResult: 5}
	n, err := m.CountRuns("proj")
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("CountRuns = %d, want 5", n)
	}
}

func TestMockRunStore_ListRuns(t *testing.T) {
	m := &MockRunStore{ListRunsResult: []domain.Run{{ID: 1}}}
	runs, err := m.ListRuns("proj", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Errorf("ListRuns returned %d runs, want 1", len(runs))
	}
}

func TestMockRunStore_ListAllRuns(t *testing.T) {
	m := &MockRunStore{ListAllRunsResult: []domain.Run{{ID: 1}, {ID: 2}}}
	runs, err := m.ListAllRuns(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Errorf("ListAllRuns returned %d runs, want 2", len(runs))
	}
}

func TestMockRunStore_CountRunsByStatus(t *testing.T) {
	m := &MockRunStore{CountByStatusResult: map[string]int{"passed": 3, "failed": 1}}
	stats, err := m.CountRunsByStatus()
	if err != nil {
		t.Fatal(err)
	}
	if stats["passed"] != 3 {
		t.Errorf("CountRunsByStatus passed = %d, want 3", stats["passed"])
	}
}

func TestMockRunStore_AsyncCallbacksAndHelpers(t *testing.T) {
	m := &MockRunStore{
		FinishRunCh: make(chan struct{}),
		ApplyTaskCh: make(chan struct{}),
	}

	finishDone := make(chan struct{})
	go func() {
		defer close(finishDone)
		_ = m.FinishRun(7, "passed", "out", "", []domain.TaskResult{{Name: "build", Status: "passed"}})
	}()

	applyDone := make(chan struct{})
	tr := domain.TaskResult{Name: "deploy", Status: "running"}
	go func() {
		defer close(applyDone)
		_, _ = m.ApplyTaskResult(8, tr, "extra")
	}()

	m.WaitFinish()
	<-finishDone
	m.WaitApply()
	<-applyDone

	if len(m.FinishRunCalls) != 1 || m.FinishRunCalls[0].ID != 7 {
		t.Fatalf("FinishRunCalls = %+v", m.FinishRunCalls)
	}
	if m.AppendTaskCallCount() != 0 {
		t.Fatalf("AppendTaskCallCount = %d, want 0", m.AppendTaskCallCount())
	}
	if err := m.AppendTaskResult(9, tr); err != nil {
		t.Fatal(err)
	}
	if m.AppendTaskCallCount() != 1 {
		t.Fatalf("AppendTaskCallCount = %d, want 1", m.AppendTaskCallCount())
	}
	if m.ApplyTaskCallCount() != 1 {
		t.Fatalf("ApplyTaskCallCount = %d, want 1", m.ApplyTaskCallCount())
	}
	if got := m.FirstApplyTaskResult(); got.Name != "deploy" || got.Status != "running" {
		t.Fatalf("FirstApplyTaskResult = %+v", got)
	}
}

func TestMockRunStore_FirstApplyResultAndCounters(t *testing.T) {
	m := &MockRunStore{}
	if got := m.FirstApplyTaskResult(); got != (domain.TaskResult{}) {
		t.Fatalf("FirstApplyTaskResult empty = %+v", got)
	}

	if err := m.AppendTaskResult(1, domain.TaskResult{Name: "build", Status: "passed"}); err != nil {
		t.Fatal(err)
	}
	if m.AppendTaskCallCount() != 1 {
		t.Fatalf("AppendTaskCallCount = %d, want 1", m.AppendTaskCallCount())
	}
	if got := m.FirstApplyTaskResult(); got != (domain.TaskResult{}) {
		t.Fatalf("FirstApplyTaskResult should still be empty before ApplyTaskResult = %+v", got)
	}
}

func TestMockPipelineRunner_RunTaskBranches(t *testing.T) {
	m := &MockPipelineRunner{}
	result, err := m.RunTask(context.Background(), model.Project{}, "lint", domain.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "lint" || result.Status != "passed" {
		t.Fatalf("RunTask default = %+v", result)
	}

	m.TaskResultList = []domain.TaskResult{{Name: "build", Status: "failed"}}
	result, err = m.RunTask(context.Background(), model.Project{}, "build", domain.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "build" || result.Status != "failed" {
		t.Fatalf("RunTask configured = %+v", result)
	}
}

func TestMockRemoteJobStore_UnmatchedPendingJobs(t *testing.T) {
	m := &MockRemoteJobStore{
		UnmatchedPendingJobsResult: []domain.UnmatchedPendingJob{
			{ID: 1, RunID: 10, TaskName: "build"},
			{ID: 2, RunID: 20, TaskName: "test"},
		},
	}
	all, err := m.UnmatchedPendingJobs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2", len(all))
	}

	run10 := 10
	filtered, err := m.UnmatchedPendingJobs(&run10)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].ID != 1 {
		t.Fatalf("filtered = %+v, want job 1", filtered)
	}

	m.UnmatchedPendingJobsErr = errors.New("boom")
	if _, err := m.UnmatchedPendingJobs(nil); err == nil {
		t.Fatal("expected error")
	}
}

var errNotFound = &notFoundErr{}

type notFoundErr struct{}

func (e *notFoundErr) Error() string { return "not found" }
