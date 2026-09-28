package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
)

// TestHandleRunProject_RejectsLocalWithoutOptIn pins
// safe-production-execution spec.md "Stored local project does not
// execute": with the opt-in off, starting a run of a project resolving to
// `local` mode fails before any source is acquired, with the shared
// explanation.
func TestHandleRunProject_RejectsLocalWithoutOptIn(t *testing.T) {
	store := &mocks.MockProjectStore{GetResult: &domain.ProjectConfig{Name: "demo", Mode: "local"}}
	srv := setupRunServer(t, store, &mocks.MockRunStore{})
	srv.runService.SetLocalExecutionPolicy(false, "local")

	w := httptest.NewRecorder()
	srv.handleRunProject(w, runCtx(httptest.NewRequest("POST", "/", strings.NewReader(`{}`)), "name", "demo"))
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "KESTRIX_RUNNER_ALLOW_LOCAL") {
		t.Fatalf("body = %s, want it to name the opt-in", w.Body.String())
	}
}

func TestHandleRunTask_RejectsLocalWithoutOptIn(t *testing.T) {
	store := &mocks.MockProjectStore{GetResult: &domain.ProjectConfig{Name: "demo", Mode: "local"}}
	srv := setupRunServer(t, store, &mocks.MockRunStore{})
	srv.runService.SetLocalExecutionPolicy(false, "local")

	w := httptest.NewRecorder()
	srv.handleRunTask(w, runCtx(httptest.NewRequest("POST", "/", nil), "name", "demo", "task", "build"))
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "KESTRIX_RUNNER_ALLOW_LOCAL") {
		t.Fatalf("body = %s, want it to name the opt-in", w.Body.String())
	}
}

func TestHandleApproveManualTask_RejectsLocalWithoutOptIn(t *testing.T) {
	run := &domain.Run{
		ID: 3, ProjectName: "demo",
		PipelineDef: &domain.Project{Name: "demo", Tasks: []domain.Task{{Name: "deploy", Manual: true}}},
		TaskResults: []domain.TaskResult{{Name: "deploy", Status: "manual"}},
	}
	store := &mocks.MockProjectStore{GetResult: &domain.ProjectConfig{Name: "demo", Mode: "local"}}
	runStore := &mocks.MockRunStore{GetRunResult: run}
	srv := setupRunServer(t, store, runStore)
	srv.runService.SetLocalExecutionPolicy(false, "local")

	w := httptest.NewRecorder()
	srv.handleApproveManualTask(w, runCtx(httptest.NewRequest("POST", "/", nil), "name", "demo", "runID", "3", "task", "deploy"))
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "KESTRIX_RUNNER_ALLOW_LOCAL") {
		t.Fatalf("body = %s, want it to name the opt-in", w.Body.String())
	}
}
