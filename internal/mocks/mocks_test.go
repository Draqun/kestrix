package mocks

import (
	"context"
	"testing"

	"gitlab.com/lunaris-bot/metaci/internal/domain"
	"gitlab.com/lunaris-bot/metaci/internal/model"
)

func TestMockProjectStore_UpsertRecordsCall(t *testing.T) {
	m := &MockProjectStore{}
	cfg := &domain.ProjectConfig{Name: "demo", RepoPath: "/x"}
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
	id, err := m.CreateRun("demo", []string{"svc"}, true, snap)
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

func TestMockWebhookLogSink_AppendAndList(t *testing.T) {
	m := &MockWebhookLogSink{}
	m.Append(map[string]interface{}{"event": "push"})
	m.Append(map[string]interface{}{"event": "merge"})
	list := m.List()
	if len(list) != 2 || list[0]["event"] != "push" || list[1]["event"] != "merge" {
		t.Errorf("List = %+v", list)
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
	if err := m.CancelRun(1); err != nil {
		t.Fatal(err)
	}
	if len(m.CancelRunCalls) != 1 || m.CancelRunCalls[0] != 1 {
		t.Errorf("CancelRunCalls = %+v", m.CancelRunCalls)
	}
}

func TestMockRunStore_ApplyTaskResult(t *testing.T) {
	m := &MockRunStore{}
	tr := domain.TaskResult{Name: "deploy", Status: "running"}
	if err := m.ApplyTaskResult(1, tr, "extra"); err != nil {
		t.Fatal(err)
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

// Sentinel error for tests.
var errNotFound = &notFoundErr{}

type notFoundErr struct{}

func (e *notFoundErr) Error() string { return "not found" }
