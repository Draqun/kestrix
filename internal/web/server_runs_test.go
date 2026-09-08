// Tests for run-shaped HTTP handlers in handler_run.go:
// handleListRuns, handleGetRun, handleCancelRun, handleListAllRuns,
// and handleRunTask covering various error modes and happy paths.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/service"

	"github.com/go-chi/chi/v5"
)

// setupRunServer returns a Server backed by the given stores for run handler tests.
func setupRunServer(t *testing.T, projectStore domain.ProjectStore, runStore domain.RunStore) *Server {
	t.Helper()
	pipeline := &mocks.MockPipelineRunner{}
	srv := &Server{
		cfg:            Config{ListenAddr: ":0", Runner: RunnerConfig{Mode: "local"}},
		projectStore:   projectStore,
		runStore:       runStore,
		pipelineRunner: pipeline,
		taskRunner:     pipeline,
		projectService: service.NewProjectService(projectStore),
		runService:     service.NewRunService(runStore, pipeline, pipeline),
		sourceSessions: &testSourceSessions{},
	}
	return srv
}

// runCtx returns a new chi context with the given URL params attached to
// the request so chi.URLParam works inside handlers.
func runCtx(r *http.Request, params ...string) *http.Request {
	ctx := chi.NewRouteContext()
	for i := 0; i+1 < len(params); i += 2 {
		ctx.URLParams.Add(params[i], params[i+1])
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, ctx))
}

// ── handleListRuns ──────────────────────────────────────────────

func TestHandleListRuns_EmptyStore(t *testing.T) {
	srv := setupRunServer(t, &mocks.MockProjectStore{}, &mocks.MockRunStore{})
	w := httptest.NewRecorder()
	r := runCtx(httptest.NewRequest("GET", "/api/projects/demo/runs", nil), "name", "demo")
	srv.handleListRuns(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	items, ok := body["items"].([]interface{})
	if !ok || len(items) != 0 {
		t.Errorf("expected empty items, got %#v", body["items"])
	}
}

func TestHandleListRuns_WithRuns(t *testing.T) {
	mock := &mocks.MockRunStore{
		CountRunsResult: 2,
		ListRunsResult: []domain.Run{
			{ID: 1, ProjectName: "demo", Status: "passed"},
			{ID: 2, ProjectName: "demo", Status: "failed"},
		},
	}
	srv := setupRunServer(t, &mocks.MockProjectStore{}, mock)
	w := httptest.NewRecorder()
	r := runCtx(httptest.NewRequest("GET", "/api/projects/demo/runs?limit=10&offset=0", nil), "name", "demo")
	srv.handleListRuns(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	items, ok := resp["items"].([]interface{})
	if !ok {
		t.Fatalf("response missing items array: %v", resp)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
	pag, ok := resp["pagination"].(map[string]interface{})
	if !ok {
		t.Fatalf("response missing pagination: %v", resp)
	}
	if pag["total"].(float64) != 2 {
		t.Errorf("pagination.total = %v, want 2", pag["total"])
	}
}

// ── handleGetRun ────────────────────────────────────────────────

func TestHandleGetRun_UnknownRun(t *testing.T) {
	srv := setupRunServer(t, &mocks.MockProjectStore{}, &mocks.MockRunStore{})
	w := httptest.NewRecorder()
	r := runCtx(httptest.NewRequest("GET", "/api/projects/demo/runs/1", nil), "name", "demo", "runID", "1")
	srv.handleGetRun(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleGetRun_InvalidID(t *testing.T) {
	srv := setupRunServer(t, &mocks.MockProjectStore{}, &mocks.MockRunStore{})
	w := httptest.NewRecorder()
	r := runCtx(httptest.NewRequest("GET", "/api/projects/demo/runs/0", nil), "name", "demo", "runID", "0")
	srv.handleGetRun(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleGetRun_NotFound(t *testing.T) {
	mock := &mocks.MockRunStore{
		GetRunResult: nil,
		GetRunErr:    nil,
	}
	srv := setupRunServer(t, &mocks.MockProjectStore{}, mock)
	w := httptest.NewRecorder()
	r := runCtx(httptest.NewRequest("GET", "/api/projects/demo/runs/999", nil), "name", "demo", "runID", "999")
	srv.handleGetRun(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleGetRun_Happy(t *testing.T) {
	now := time.Now()
	mock := &mocks.MockRunStore{
		GetRunResult: &domain.Run{
			ID:          1,
			ProjectName: "demo",
			Status:      "passed",
			StartedAt:   now,
		},
	}
	srv := setupRunServer(t, &mocks.MockProjectStore{}, mock)
	w := httptest.NewRecorder()
	r := runCtx(httptest.NewRequest("GET", "/api/projects/demo/runs/1", nil), "name", "demo", "runID", "1")
	srv.handleGetRun(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var run domain.Run
	if err := json.NewDecoder(w.Body).Decode(&run); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if run.ID != 1 {
		t.Errorf("run.ID = %d, want 1", run.ID)
	}
	if run.Status != "passed" {
		t.Errorf("run.Status = %q, want passed", run.Status)
	}
}

// ── handleCancelRun ─────────────────────────────────────────────

func TestHandleCancelRun_EmptyStore(t *testing.T) {
	srv := setupRunServer(t, &mocks.MockProjectStore{}, &mocks.MockRunStore{})
	w := httptest.NewRecorder()
	r := runCtx(httptest.NewRequest("POST", "/api/projects/demo/runs/1/cancel", nil), "name", "demo", "runID", "1")
	srv.handleCancelRun(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestHandleCancelRun_InvalidID(t *testing.T) {
	srv := setupRunServer(t, &mocks.MockProjectStore{}, &mocks.MockRunStore{})
	w := httptest.NewRecorder()
	r := runCtx(httptest.NewRequest("POST", "/api/projects/demo/runs/0/cancel", nil), "name", "demo", "runID", "0")
	srv.handleCancelRun(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleCancelRun_Happy(t *testing.T) {
	mock := &mocks.MockRunStore{}
	srv := setupRunServer(t, &mocks.MockProjectStore{}, mock)
	manager := srv.sourceSessions.(*testSourceSessions)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/projects/demo/runs/5/cancel", nil)
	req = req.WithContext(contextWithClaims(req.Context(), domain.Claims{UserName: "carol", Role: domain.RoleOperator}))
	r := runCtx(req, "name", "demo", "runID", "5")
	srv.handleCancelRun(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if len(mock.CancelRunCalls) != 1 {
		t.Fatalf("expected 1 CancelRun call, got %d", len(mock.CancelRunCalls))
	}
	if mock.CancelRunCalls[0].ID != 5 {
		t.Errorf("CancelRun called with %d, want 5", mock.CancelRunCalls[0].ID)
	}
	if mock.CancelRunCalls[0].CanceledBy != "user:carol" {
		t.Errorf("CanceledBy = %q, want user:carol", mock.CancelRunCalls[0].CanceledBy)
	}
	if len(manager.released) != 1 || manager.released[0] != 5 {
		t.Fatalf("released pins = %v, want [5]", manager.released)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "canceled" {
		t.Errorf("status = %q, want canceled", resp["status"])
	}
}

func TestHandleCancelRun_NoRunService(t *testing.T) {
	srv := setupRunServer(t, &mocks.MockProjectStore{}, &mocks.MockRunStore{})
	srv.runService = nil
	w := httptest.NewRecorder()
	r := runCtx(httptest.NewRequest("POST", "/api/projects/demo/runs/1/cancel", nil), "name", "demo", "runID", "1")
	srv.handleCancelRun(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body=%s)", w.Code, w.Body.String())
	}
}

// blockingCancelRunner blocks Run until releaseCh closes, ignoring context
// cancellation - simulating a run that does not terminate within its grace
// period, for handleCancelRun's timeout path.
type blockingCancelRunner struct {
	startedCh chan struct{}
	releaseCh chan struct{}
}

func (r *blockingCancelRunner) Run(_ context.Context, _ domain.Project, _ map[string]bool, _ domain.RunnerOptions) ([]domain.TaskResult, error) {
	close(r.startedCh)
	<-r.releaseCh
	return nil, nil
}

func (r *blockingCancelRunner) RunTask(_ context.Context, _ domain.Project, _ string, _ domain.RunnerOptions) (domain.TaskResult, error) {
	return domain.TaskResult{}, nil
}

func TestHandleCancelRun_Timeout(t *testing.T) {
	store := &mocks.MockRunStore{CreateRunID: 99}
	runner := &blockingCancelRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{})}
	svc := service.NewRunService(store, runner, runner)
	svc.CancelGrace = time.Millisecond
	oldBuffer := service.CancelWaitBuffer
	service.CancelWaitBuffer = 50 * time.Millisecond
	t.Cleanup(func() { service.CancelWaitBuffer = oldBuffer })

	srv := &Server{
		cfg:            Config{ListenAddr: ":0", Runner: RunnerConfig{Mode: "local"}},
		runStore:       store,
		runService:     svc,
		sourceSessions: &testSourceSessions{},
	}

	runID, err := svc.Start(context.Background(), "demo", &domain.Project{Name: "demo"}, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-runner.startedCh
	t.Cleanup(func() { close(runner.releaseCh) })

	w := httptest.NewRecorder()
	r := runCtx(httptest.NewRequest("POST", "/api/projects/demo/runs/99/cancel", nil), "name", "demo", "runID", strconv.Itoa(runID))
	srv.handleCancelRun(w, r)

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504 (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "did not stop in time") {
		t.Errorf("body = %q, want it to say the run did not stop in time", w.Body.String())
	}
	if len(store.CancelRunCalls) != 0 {
		t.Errorf("CancelRun should not be called on timeout, got %v", store.CancelRunCalls)
	}
}

// ── handleListAllRuns ───────────────────────────────────────────

func TestHandleListAllRuns_EmptyStore(t *testing.T) {
	srv := setupRunServer(t, &mocks.MockProjectStore{}, &mocks.MockRunStore{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/runs", nil)
	srv.handleListAllRuns(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body []domain.Run
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("expected empty array, got %d items", len(body))
	}
}

func TestHandleListAllRuns_Happy(t *testing.T) {
	mock := &mocks.MockRunStore{
		ListAllRunsResult: []domain.Run{
			{ID: 1, ProjectName: "a", Status: "passed"},
			{ID: 2, ProjectName: "b", Status: "failed"},
			{ID: 3, ProjectName: "a", Status: "running"},
		},
	}
	srv := setupRunServer(t, &mocks.MockProjectStore{}, mock)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/runs", nil)
	srv.handleListAllRuns(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var runs []domain.Run
	if err := json.NewDecoder(w.Body).Decode(&runs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(runs) != 3 {
		t.Errorf("expected 3 runs, got %d", len(runs))
	}
}

// ── handleRunTask ───────────────────────────────────────────────

// runTaskServer creates a Server with a mock project store and a temp
// directory containing a minimal pipeline.tf. The returned cleanup
// function removes the temp dir.
func runTaskServer(t *testing.T, projectConfig *domain.ProjectConfig, runStore domain.RunStore) *Server {
	t.Helper()
	pipeline := &mocks.MockPipelineRunner{TaskResultList: []domain.TaskResult{{Name: "test", Status: "passed"}}}
	srv := &Server{
		cfg: Config{
			ListenAddr: ":0",
			Runner:     RunnerConfig{Mode: "local"},
		},
		projectStore:   &mocks.MockProjectStore{GetResult: projectConfig},
		runStore:       runStore,
		pipelineRunner: pipeline,
		taskRunner:     pipeline,
		projectService: service.NewProjectService(&mocks.MockProjectStore{}),
		runService:     service.NewRunService(runStore, pipeline, pipeline),
		sourceSessions: &testSourceSessions{},
	}
	return srv
}

func TestHandleRunTask_NotFound(t *testing.T) {
	mock := &mocks.MockProjectStore{GetResult: nil}
	srv := setupRunServer(t, mock, nil)
	w := httptest.NewRecorder()
	r := runCtx(httptest.NewRequest("POST", "/api/projects/nope/tasks/test/run", nil),
		"name", "nope", "task", "test")
	srv.handleRunTask(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleRunTask_InvalidJSON(t *testing.T) {
	tempDir := t.TempDir()
	pipelineDir := filepath.Join(tempDir, "ci")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pipelineDir, "pipeline.tf"), []byte(`project "demo" {
  ci_repo "main" {
    path = "."
  }
  ci_task "test" {
    trigger = "repo.main.changed"
    execute {
      repo = "main"
      script = "echo ok"
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &domain.ProjectConfig{
		Name:         "demo",
		RepoURL:      "file://" + tempDir,
		RepoRef:      "main",
		PipelineDir:  "ci",
		PipelineFile: "pipeline.tf",
	}
	srv := runTaskServer(t, cfg, &mocks.MockRunStore{CreateRunID: 1})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects/demo/tasks/test/run", strings.NewReader("not json"))
	r = runCtx(r, "name", "demo", "task", "test")
	srv.handleRunTask(w, r)

	// handleRunTask does not decode a request body, so invalid JSON is
	// ignored and the task runs successfully.
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
}

func TestHandleRunTask_Happy(t *testing.T) {
	tempDir := t.TempDir()
	pipelineDir := filepath.Join(tempDir, "ci")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pipelineDir, "pipeline.tf"), []byte(`project "demo" {
  ci_repo "main" {
    path = "."
  }
  ci_task "test" {
    trigger = "repo.main.changed"
    execute {
      repo = "main"
      script = "echo hello-from-task"
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := &mocks.MockRunStore{CreateRunID: 1}
	cfg := &domain.ProjectConfig{
		Name:         "demo",
		RepoURL:      "file://" + tempDir,
		RepoRef:      "main",
		PipelineDir:  "ci",
		PipelineFile: "pipeline.tf",
	}
	srv := runTaskServer(t, cfg, mock)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects/demo/tasks/test/run", strings.NewReader(`{}`))
	r = r.WithContext(contextWithClaims(r.Context(), domain.Claims{UserName: "dave", Role: domain.RoleOperator}))
	r = runCtx(r, "name", "demo", "task", "test")
	srv.handleRunTask(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["project"] != "demo" {
		t.Errorf("project = %v, want demo", resp["project"])
	}
	if resp["task"] != "test" {
		t.Errorf("task = %v, want test", resp["task"])
	}
	if resp["status"] != "passed" {
		t.Errorf("status = %v, want passed", resp["status"])
	}
	if len(mock.CreateRunCalls) != 1 || mock.CreateRunCalls[0].StartedBy != "user:dave" {
		t.Fatalf("CreateRunCalls = %+v, want StartedBy=user:dave", mock.CreateRunCalls)
	}
	// handleRunTask shares RunService.RunTask with the manual-approval path
	// (design.md D4), so re-running a task directly by name stamps the same
	// approver fields on its result - see REVIEW.md R1/R2.
	if len(mock.FinishRunCalls) != 1 || len(mock.FinishRunCalls[0].TaskResults) != 1 {
		t.Fatalf("FinishRunCalls = %+v, want 1 call with 1 task result", mock.FinishRunCalls)
	}
	tr := mock.FinishRunCalls[0].TaskResults[0]
	if tr.ApprovedBy != "user:dave" {
		t.Errorf("ApprovedBy = %q, want user:dave", tr.ApprovedBy)
	}
	if tr.ApprovedAt == nil {
		t.Error("ApprovedAt = nil, want set")
	}
}

// ── handleApproveManualTask ──────────────────────────────────────

func TestHandleApproveManualTask_UnknownRun(t *testing.T) {
	srv := setupRunServer(t, &mocks.MockProjectStore{}, &mocks.MockRunStore{})
	w := httptest.NewRecorder()
	r := runCtx(httptest.NewRequest("POST", "/api/projects/demo/runs/1/tasks/deploy/approve", nil),
		"name", "demo", "runID", "1", "task", "deploy")
	srv.handleApproveManualTask(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleApproveManualTask_InvalidRunID(t *testing.T) {
	srv := setupRunServer(t, &mocks.MockProjectStore{}, &mocks.MockRunStore{})
	w := httptest.NewRecorder()
	r := runCtx(httptest.NewRequest("POST", "/api/projects/demo/runs/0/tasks/deploy/approve", nil),
		"name", "demo", "runID", "0", "task", "deploy")
	srv.handleApproveManualTask(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleApproveManualTask_RunNotFound(t *testing.T) {
	mock := &mocks.MockRunStore{GetRunResult: nil}
	srv := setupRunServer(t, &mocks.MockProjectStore{}, mock)
	w := httptest.NewRecorder()
	r := runCtx(httptest.NewRequest("POST", "/api/projects/demo/runs/99/tasks/deploy/approve", nil),
		"name", "demo", "runID", "99", "task", "deploy")
	srv.handleApproveManualTask(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleApproveManualTask_TaskNotFound(t *testing.T) {
	mock := &mocks.MockRunStore{
		GetRunResult: &domain.Run{
			ID:          1,
			ProjectName: "demo",
			TaskResults: []domain.TaskResult{
				{Name: "build", Status: "passed"},
			},
		},
	}
	srv := setupRunServer(t, &mocks.MockProjectStore{}, mock)
	w := httptest.NewRecorder()
	r := runCtx(httptest.NewRequest("POST", "/api/projects/demo/runs/1/tasks/deploy/approve", nil),
		"name", "demo", "runID", "1", "task", "deploy")
	srv.handleApproveManualTask(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleApproveManualTask_TaskNotWaiting(t *testing.T) {
	mock := &mocks.MockRunStore{
		GetRunResult: &domain.Run{
			ID:          1,
			ProjectName: "demo",
			TaskResults: []domain.TaskResult{
				{Name: "deploy", Status: "passed"},
			},
		},
	}
	srv := setupRunServer(t, &mocks.MockProjectStore{}, mock)
	w := httptest.NewRecorder()
	r := runCtx(httptest.NewRequest("POST", "/api/projects/demo/runs/1/tasks/deploy/approve", nil),
		"name", "demo", "runID", "1", "task", "deploy")
	srv.handleApproveManualTask(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleApproveManualTaskSnapshotAndSourceErrors(t *testing.T) {
	baseRun := &domain.Run{
		ID: 1, ProjectName: "demo",
		TaskResults: []domain.TaskResult{{Name: "deploy", Status: "manual"}},
	}
	store := &mocks.MockRunStore{GetRunResult: baseRun}
	srv := setupRunServer(t, &mocks.MockProjectStore{}, store)
	w := httptest.NewRecorder()
	srv.handleApproveManualTask(w, runCtx(httptest.NewRequest("POST", "/", nil), "name", "demo", "runID", "1", "task", "deploy"))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "snapshot") {
		t.Fatalf("snapshot status=%d body=%s", w.Code, w.Body.String())
	}

	baseRun.PipelineDef = &domain.Project{Name: "demo", Tasks: []domain.Task{{Name: "deploy", Manual: true}}}
	baseRun.Sources = []domain.AcquiredSource{{RepoName: "demo", URL: "https://git.test/demo.git", RequestedRef: "main", ResolvedRevision: "sha", Primary: true}}
	srv.sourceSessions = &testSourceSessions{err: errors.New("exact source unavailable")}
	w = httptest.NewRecorder()
	srv.handleApproveManualTask(w, runCtx(httptest.NewRequest("POST", "/", nil), "name", "demo", "runID", "1", "task", "deploy"))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "exact source") {
		t.Fatalf("source status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHandleApproveManualTaskRunnerFailure(t *testing.T) {
	run := &domain.Run{
		ID: 2, ProjectName: "demo",
		PipelineDef: &domain.Project{Name: "demo", Tasks: []domain.Task{{Name: "deploy", Manual: true}}},
		TaskResults: []domain.TaskResult{{Name: "deploy", Status: "manual"}},
		Sources:     []domain.AcquiredSource{{RepoName: "demo", URL: "https://git.test/demo.git", RequestedRef: "main", ResolvedRevision: "sha", Primary: true}},
	}
	store := &mocks.MockRunStore{GetRunResult: run}
	runner := &mocks.MockPipelineRunner{RunErr: errors.New("manual runner failed")}
	srv := setupRunServer(t, &mocks.MockProjectStore{}, store)
	srv.taskRunner = runner
	srv.runService = service.NewRunService(store, runner, runner)
	w := httptest.NewRecorder()
	srv.handleApproveManualTask(w, runCtx(httptest.NewRequest("POST", "/", nil), "name", "demo", "runID", "2", "task", "deploy"))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "manual runner failed") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHandleApproveManualTask_Happy(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "pipeline.tf"), []byte(`project "test" {
  ci_repo "app" {
    path = "."
  }
  ci_task "build" {
    trigger = "repo.app.changed"
    execute {
      repo = "app"
      script = "echo build"
    }
  }
  ci_task "deploy" {
    trigger = "repo.app.changed"
    manual = true
    depends_on = "build"
    execute {
      repo = "app"
      script = "echo deploy"
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	mockRun := &mocks.MockRunStore{
		ApplyTaskCh: make(chan struct{}),
		GetRunResult: &domain.Run{
			ID:          1,
			ProjectName: "test",
			PipelineDef: &domain.Project{
				Name:  "test",
				Repos: []domain.Repo{{Name: "app", Path: "."}},
				Tasks: []domain.Task{
					{Name: "build", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo build"}},
					{Name: "deploy", Manual: true, DependsOn: []string{"build"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "echo deploy"}},
				},
			},
			Sources: []domain.AcquiredSource{{
				RepoName: "test", URL: "https://git.example.test/test.git", RequestedRef: "main",
				ResolvedRevision: "0123456789abcdef", Primary: true,
			}},
			TaskResults: []domain.TaskResult{
				{Name: "build", Status: "passed"},
				{Name: "deploy", Status: "manual"},
			},
		},
	}
	mockProj := &mocks.MockProjectStore{
		GetResult: &domain.ProjectConfig{
			Name:         "test",
			RepoURL:      "file://" + tmp,
			RepoRef:      "main",
			PipelineDir:  ".",
			PipelineFile: "pipeline.tf",
		},
	}
	srv := setupRunServer(t, mockProj, mockRun)
	srv.sourceSessions = &testSourceSessions{root: tmp}
	pipeline := srv.taskRunner.(*mocks.MockPipelineRunner)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/projects/test/runs/1/tasks/deploy/approve", nil)
	req = req.WithContext(contextWithClaims(req.Context(), domain.Claims{UserName: "erin", Role: domain.RoleOperator}))
	r := runCtx(req, "name", "test", "runID", "1", "task", "deploy")
	srv.handleApproveManualTask(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["task"] != "deploy" {
		t.Errorf("task = %v, want deploy", resp["task"])
	}
	if resp["status"] != "running" {
		t.Errorf("status = %v, want running", resp["status"])
	}

	// The synchronous ApplyTaskResult("running") at handler line 439 is
	// guaranteed to have completed before handleApproveManualTask returns.
	// WaitApply blocks until the first close of ApplyTaskCh and takes the
	// mutex to establish a happens-before edge, so reading the slice under
	// the lock is safe even while the background goroutine may append again.
	mockRun.WaitApply()

	callCount := mockRun.ApplyTaskCallCount()
	if callCount < 1 {
		t.Fatalf("expected at least 1 ApplyTaskResult call, got %d", callCount)
	}
	firstStatus := mockRun.FirstApplyTaskResult().Status
	if firstStatus != "running" {
		t.Errorf("first ApplyTaskResult status = %q, want running", firstStatus)
	}
	if firstApproved := mockRun.FirstApplyTaskResult().ApprovedBy; firstApproved != "user:erin" {
		t.Errorf("first ApplyTaskResult ApprovedBy = %q, want user:erin", firstApproved)
	}
	if len(pipeline.TaskRunCalls) != 1 || pipeline.TaskRunCalls[0].Actor != "user:erin" {
		t.Fatalf("TaskRunCalls = %+v, want Actor=user:erin", pipeline.TaskRunCalls)
	}
}

func TestManualApprovalUsesStoredSnapshotAndExactRecordedSources(t *testing.T) {
	root := t.TempDir()
	snapshot := &domain.Project{
		Name: "demo",
		Repos: []domain.Repo{
			{Name: "app", Path: "."},
			{Name: "docs", URL: "https://git.example.test/docs.git", Ref: "main"},
		},
		Tasks: []domain.Task{{Name: "deploy", Manual: true, Execute: domain.ExecuteSpec{Repo: "app", Script: "echo snapshot"}}},
	}
	run := &domain.Run{
		ID: 73, ProjectName: "demo", PipelineDef: snapshot,
		TaskResults: []domain.TaskResult{{Name: "deploy", Status: "manual"}},
		Sources: []domain.AcquiredSource{
			{RepoName: "demo", URL: "https://git.example.test/demo.git", RequestedRef: "main", ResolvedRevision: "primary-old-sha", Primary: true},
			{RepoName: "docs", URL: "https://git.example.test/docs.git", RequestedRef: "main", ResolvedRevision: "docs-old-sha"},
		},
	}
	runStore := &mocks.MockRunStore{GetRunResult: run}
	srv := setupRunServer(t, &mocks.MockProjectStore{GetResult: &domain.ProjectConfig{
		Name: "demo", RepoURL: "https://git.example.test/demo.git", RepoRef: "moved-branch",
	}}, runStore)
	manager := &testSourceSessions{root: root}
	srv.sourceSessions = manager
	runner := &mocks.MockPipelineRunner{TaskResultList: []domain.TaskResult{{Name: "deploy", Status: "passed"}}}
	srv.taskRunner = runner
	srv.runService = service.NewRunService(runStore, runner, runner)

	w := httptest.NewRecorder()
	srv.handleApproveManualTask(w, runCtx(httptest.NewRequest("POST", "/", nil), "name", "demo", "runID", "73", "task", "deploy"))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(manager.requests) != 2 || manager.requests[0].Revision != "primary-old-sha" || manager.requests[1].Revision != "docs-old-sha" {
		t.Fatalf("source requests = %+v", manager.requests)
	}
	if len(runner.TaskRunCalls) != 1 || runner.TaskRunCalls[0].Project.Tasks[0].Execute.Script != "echo snapshot" {
		t.Fatalf("task calls = %+v", runner.TaskRunCalls)
	}
	if manager.cleaned != 2 {
		t.Fatalf("cleaned sessions = %d, want 2", manager.cleaned)
	}
}

// ── handleRunProject ─────────────────────────────────────────────

func TestHandleRunProject_NotFound(t *testing.T) {
	mock := &mocks.MockProjectStore{GetResult: nil}
	srv := setupRunServer(t, mock, nil)
	w := httptest.NewRecorder()
	r := runCtx(httptest.NewRequest("POST", "/api/projects/nope/run", strings.NewReader(`{}`)),
		"name", "nope")
	srv.handleRunProject(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleRunProject_Happy(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "pipeline.tf"), []byte(`project "demo" {
  ci_repo "main" {
    path = "."
  }
  ci_task "test" {
    trigger = "repo.main.changed"
    execute {
      repo = "main"
      script = "echo ok"
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	mockRun := &mocks.MockRunStore{CreateRunID: 42, FinishRunCh: make(chan struct{})}
	mockProj := &mocks.MockProjectStore{
		GetResult: &domain.ProjectConfig{
			Name:         "demo",
			RepoURL:      "file://" + tmp,
			RepoRef:      "main",
			PipelineDir:  ".",
			PipelineFile: "pipeline.tf",
		},
	}
	srv := setupRunServer(t, mockProj, mockRun)
	srv.sourceSessions = &testSourceSessions{root: tmp}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/projects/demo/run", strings.NewReader(`{"changed":["main"]}`))
	req = req.WithContext(contextWithClaims(req.Context(), domain.Claims{UserName: "frank", Role: domain.RoleOperator}))
	r := runCtx(req, "name", "demo")
	srv.handleRunProject(w, r)

	<-mockRun.FinishRunCh // wait for background goroutine

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["project"] != "demo" {
		t.Errorf("project = %v, want demo", resp["project"])
	}
	if resp["run_id"].(float64) != 42 {
		t.Errorf("run_id = %v, want 42", resp["run_id"])
	}
	if resp["status"] != "running" {
		t.Errorf("status = %v, want running", resp["status"])
	}
	if mockRun.AppendTaskCallCount() < 1 {
		t.Fatalf("expected at least 1 AppendTaskResult call, got %d", mockRun.AppendTaskCallCount())
	}
	if len(mockRun.CreateRunCalls) != 1 || mockRun.CreateRunCalls[0].StartedBy != "user:frank" {
		t.Fatalf("CreateRunCalls = %+v, want StartedBy=user:frank", mockRun.CreateRunCalls)
	}
}

// TestHandleRunProject_ActorNotSpoofable is the run-attribution "caller-
// supplied actor is ignored" scenario: a start request carrying an actor
// name in its body and an X-Actor header still records the authenticated
// account only.
func TestHandleRunProject_ActorNotSpoofable(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "pipeline.tf"), []byte(`project "demo" {
  ci_repo "main" {
    path = "."
  }
  ci_task "test" {
    trigger = "repo.main.changed"
    execute {
      repo = "main"
      script = "echo ok"
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	mockRun := &mocks.MockRunStore{CreateRunID: 43, FinishRunCh: make(chan struct{})}
	mockProj := &mocks.MockProjectStore{
		GetResult: &domain.ProjectConfig{
			Name:         "demo",
			RepoURL:      "file://" + tmp,
			RepoRef:      "main",
			PipelineDir:  ".",
			PipelineFile: "pipeline.tf",
		},
	}
	srv := setupRunServer(t, mockProj, mockRun)
	srv.sourceSessions = &testSourceSessions{root: tmp}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/projects/demo/run", strings.NewReader(`{"changed":["main"],"started_by":"user:mallory"}`))
	req.Header.Set("X-Actor", "user:mallory")
	req = req.WithContext(contextWithClaims(req.Context(), domain.Claims{UserName: "frank", Role: domain.RoleOperator}))
	r := runCtx(req, "name", "demo")
	srv.handleRunProject(w, r)

	<-mockRun.FinishRunCh

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if len(mockRun.CreateRunCalls) != 1 || mockRun.CreateRunCalls[0].StartedBy != "user:frank" {
		t.Fatalf("CreateRunCalls = %+v, want StartedBy=user:frank (not user:mallory)", mockRun.CreateRunCalls)
	}
}
