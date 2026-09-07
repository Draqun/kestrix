// Tests for handler_system, handler_webhook, handler_project (save pipeline,
// resolve project), and handler_pipeline. Covers mock-based scenarios and
// edge cases not exercised by the existing test files.
package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kestrix/internal/auth"
	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/service"
)

// ─── helpers ─────────────────────────────────────────────────────────

// webhookProjectStore wraps MockProjectStore so we can override
// FindByGitLabID for webhook tests that need a project match.
type webhookProjectStore struct {
	*mocks.MockProjectStore
	findResult *domain.ProjectConfig
	findErr    error
}

func (w *webhookProjectStore) FindByGitLabID(id int) (*domain.ProjectConfig, error) {
	return w.findResult, w.findErr
}

// pushPayload builds a minimal GitLab push webhook JSON body.
func pushPayload(projectID int, repo, branch string, files []string) string {
	var parts []string
	for i, f := range files {
		id := string(rune('a' + i))
		parts = append(parts, `{"id":"`+id+`","message":"m","added":["`+f+`"],"modified":[],"removed":[]}`)
	}
	commits := strings.Join(parts, ",")
	return "{" +
		`"object_kind":"push",` +
		`"ref":"refs/heads/` + branch + `",` +
		`"project":{"id":` + intToStr(projectID) + `,"path_with_namespace":"` + repo + `","default_branch":"main"},` +
		`"commits":[` + commits + `]` +
		`}`
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// ─── Dashboard ───────────────────────────────────────────────────────

func TestHandleDashboard_WithRunStore(t *testing.T) {
	tmpDir := t.TempDir()
	mockProj := &mocks.MockProjectStore{
		ListResult: []string{"proj-a", "proj-b"},
	}
	mockRun := &mocks.MockRunStore{
		CountByStatusResult: map[string]int{"passed": 5, "failed": 2},
		ListAllRunsResult: []domain.Run{
			{ID: 1, ProjectName: "proj-a", Status: "passed"},
			{ID: 2, ProjectName: "proj-b", Status: "failed"},
		},
	}
	srv := &Server{
		cfg:            Config{ListenAddr: ":0", PipelineDir: tmpDir},
		projectStore:   mockProj,
		runStore:       mockRun,
		projectService: service.NewProjectService(mockProj),
	}
	srv.router = srv.buildRouter()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/dashboard", nil)
	srv.handleDashboard(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["pg_available"] != true {
		t.Errorf("pg_available = %v, want true", body["pg_available"])
	}
	if body["project_count"].(float64) != 2 {
		t.Errorf("project_count = %v, want 2", body["project_count"])
	}
	runStats, ok := body["run_stats"].(map[string]interface{})
	if !ok {
		t.Fatalf("run_stats type = %T, want map", body["run_stats"])
	}
	if runStats["passed"].(float64) != 5 {
		t.Errorf("run_stats.passed = %v, want 5", runStats["passed"])
	}
	recentRuns, ok := body["recent_runs"].([]interface{})
	if !ok {
		t.Fatalf("recent_runs type = %T, want slice", body["recent_runs"])
	}
	if len(recentRuns) != 2 {
		t.Errorf("recent_runs len = %d, want 2", len(recentRuns))
	}
}

func TestHandleDashboard_EmptyRunStore(t *testing.T) {
	mockProj := &mocks.MockProjectStore{ListResult: []string{"a"}}
	srv := &Server{
		cfg:            Config{ListenAddr: ":0"},
		projectStore:   mockProj,
		runStore:       &mocks.MockRunStore{},
		projectService: service.NewProjectService(mockProj),
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/dashboard", nil)
	srv.handleDashboard(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["pg_available"] != true {
		t.Errorf("pg_available = %v, want true for PostgreSQL store", body["pg_available"])
	}
}

// ─── SaveProjectPipeline ─────────────────────────────────────────────

func TestHandleSaveProjectPipeline_BadBody(t *testing.T) {
	tmpDir := t.TempDir()
	srv := systemHandlerServer(t)
	if err := srv.projectStore.Upsert(&domain.ProjectConfig{
		Name:         "demo",
		RepoURL:      "file://" + tmpDir,
		RepoRef:      "main",
		PipelineDir:  ".",
		PipelineFile: "pipeline.tf",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects/demo/pipeline",
		strings.NewReader(""))
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 (body=%s)", w.Code, w.Body.String())
	}
}

// ─── ResolveProject ──────────────────────────────────────────────────

func TestHandleResolveProject_Happy(t *testing.T) {
	tmpDir := t.TempDir()
	writeTaskPipeline(t, tmpDir)

	mockProj := &mocks.MockProjectStore{
		GetResult: &domain.ProjectConfig{
			Name:        "demo",
			RepoURL:     "file://" + tmpDir,
			RepoRef:     "main",
			PipelineDir: ".",
		},
	}
	srv := &Server{
		cfg:            Config{ListenAddr: ":0", PipelineDir: tmpDir},
		projectStore:   mockProj,
		projectService: service.NewProjectService(mockProj),
		sourceSessions: &testSourceSessions{},
		tokenIssuer:    auth.JWTIssuer{Key: testSigningKey},
	}
	srv.router = srv.buildRouter()

	body := `{"changed":["svc"]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects/demo/resolve", strings.NewReader(body))
	srv.router.ServeHTTP(w, authed(t, srv, r))

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
	triggered, ok := resp["triggered"].([]interface{})
	if !ok {
		t.Fatalf("triggered type = %T, want slice", resp["triggered"])
	}
	if len(triggered) == 0 {
		t.Errorf("triggered tasks = 0, want at least 1")
	}
}

func TestHandleResolveProject_NotFound(t *testing.T) {
	srv := systemHandlerServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects/nope/resolve",
		strings.NewReader(`{"changed":["svc"]}`))
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// ─── Webhook ─────────────────────────────────────────────────────────

func TestHandleWebhook_UnknownProject(t *testing.T) {
	tmpDir := t.TempDir()
	// Write a pipeline so the project store can resolve it if needed,
	// but FindByGitLabID will return nil so the handler returns early.
	writeTaskPipeline(t, tmpDir)

	mockProj := &webhookProjectStore{
		MockProjectStore: &mocks.MockProjectStore{},
		findResult:       nil,
	}
	srv := &Server{
		cfg:            Config{ListenAddr: ":0", PipelineDir: tmpDir},
		projectStore:   mockProj,
		projectService: service.NewProjectService(mockProj),
	}

	payload := pushPayload(42, "org/unknown", "main", []string{"main.go"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	srv.handleWebhook(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "ignored" {
		t.Errorf("status = %v, want ignored", resp["status"])
	}
	if resp["reason"] != "unknown project" {
		t.Errorf("reason = %v, want 'unknown project'", resp["reason"])
	}
}

func TestHandleWebhook_NotPush(t *testing.T) {
	srv := systemHandlerServer(t)
	payload := `{"object_kind":"merge_request","ref":"refs/heads/main","project":{},"commits":[]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	srv.handleWebhook(w, r)

	// ParseGitLabPushEvent returns an error for non-push events, which
	// the handler surfaces as 400 Bad Request.
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "error") {
		t.Errorf("body = %q, want error message", w.Body.String())
	}
}

func TestHandleWebhook_Happy(t *testing.T) {
	tmpDir := t.TempDir()
	writeTaskPipeline(t, tmpDir)

	mockProj := &webhookProjectStore{
		MockProjectStore: &mocks.MockProjectStore{},
		findResult: &domain.ProjectConfig{
			Name:        "demo",
			RepoURL:     "file://" + tmpDir,
			RepoRef:     "main",
			PipelineDir: ".",
		},
	}
	srv := &Server{
		cfg:            Config{ListenAddr: ":0", PipelineDir: tmpDir},
		projectStore:   mockProj,
		projectService: service.NewProjectService(mockProj),
		sourceSessions: &testSourceSessions{},
	}

	// "svc/main.go" matches the "svc" subpath repo (path="src") only if
	// the file is under "src/". The pipelineWithTaskTF uses path="src" for
	// the "svc" repo, so we change "src/main.go" to match.
	payload := pushPayload(42, "org/test", "main", []string{"src/main.go"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	srv.handleWebhook(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status = %v, want ok", resp["status"])
	}
	if resp["project"] != "demo" {
		t.Errorf("project = %v, want demo", resp["project"])
	}
	tasks, ok := resp["tasks"].([]interface{})
	if !ok {
		t.Fatalf("tasks type = %T, want slice", resp["tasks"])
	}
	if len(tasks) == 0 {
		t.Errorf("triggered tasks = 0, want at least 1")
	}
	if resp["dispatched"] != false {
		t.Errorf("dispatched = %v, want false (dispatch not configured)", resp["dispatched"])
	}
}
