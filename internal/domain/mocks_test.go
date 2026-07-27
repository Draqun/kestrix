package domain

import (
	"context"
	"testing"

	"gitlab.com/lunaris-bot/metaci/internal/model"
)

func TestMockProjectStore_UpsertRecordsCall(t *testing.T) {
	m := &MockProjectStore{}
	cfg := &ProjectConfig{Name: "demo", RepoPath: "/x"}
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
	trs := []TaskResult{{Name: "build", Status: "passed"}}
	_ = m.FinishRun(1, "passed", "out", "", trs)
	if len(m.FinishRunCalls) != 1 || m.FinishRunCalls[0].Status != "passed" {
		t.Errorf("FinishRunCalls = %+v", m.FinishRunCalls)
	}
}

func TestMockPipelineRunner_ReturnsConfiguredResults(t *testing.T) {
	m := &MockPipelineRunner{TaskResultList: []TaskResult{{Name: "t", Status: "passed"}}}
	results, err := m.Run(context.Background(), model.Project{}, nil, RunnerOptions{})
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
	_, _ = m.Run(context.Background(), model.Project{}, nil, RunnerOptions{})
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

// Sentinel error for tests.
var errNotFound = &notFoundErr{}

type notFoundErr struct{}

func (e *notFoundErr) Error() string { return "not found" }