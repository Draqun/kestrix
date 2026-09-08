package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
)

// TestReadVisibility_ViewerWithNoGrantsSeesEveryProjectRunAndEvent pins
// design.md D6: read visibility is global. A global viewer holding no
// project grants at all still sees every project, every run, and every
// webhook event - grants only ever raise a role, never scope a list; see
// openspec spec access-control, "Cross-project read visibility".
func TestReadVisibility_ViewerWithNoGrantsSeesEveryProjectRunAndEvent(t *testing.T) {
	srv := NewServer(Config{})
	projectStore := srv.projectStore.(*mocks.MockProjectStore)
	projectStore.Projects = map[string]*domain.ProjectConfig{
		"alpha": {Name: "alpha", RepoURL: "https://git.test/alpha.git", RepoRef: "main"},
		"beta":  {Name: "beta", RepoURL: "https://git.test/beta.git", RepoRef: "main"},
	}
	runStore := srv.runStore.(*mocks.MockRunStore)
	runStore.ListAllRunsResult = []domain.Run{
		{ID: 1, ProjectName: "alpha", Status: "success"},
		{ID: 2, ProjectName: "beta", Status: "success"},
	}
	webhookLog := srv.webhookLogStore.(*mocks.MockWebhookLogSink)
	webhookLog.Entries = []domain.WebhookEvent{
		{ID: 1, ProjectName: "alpha", Status: domain.WebhookStatusTriggered},
		{ID: 2, ProjectName: "beta", Status: domain.WebhookStatusTriggered},
	}

	token := authToken(t, srv, domain.RoleViewer) // no grant on either project

	// Projects
	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	req.Header.Set("Authorization", token)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/projects status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var projectsResp struct {
		Items []domain.ProjectConfig `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &projectsResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(projectsResp.Items) != 2 {
		t.Fatalf("projects returned = %d, want 2 (both alpha and beta, ungated by grants)", len(projectsResp.Items))
	}

	// Runs
	req = httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	req.Header.Set("Authorization", token)
	w = httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/runs status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var runs []domain.Run
	if err := json.Unmarshal(w.Body.Bytes(), &runs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs returned = %d, want 2 (both projects' runs, ungated by grants)", len(runs))
	}

	// Webhook events
	req = httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	req.Header.Set("Authorization", token)
	w = httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/logs status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var logsResp struct {
		Items []domain.WebhookEvent `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &logsResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(logsResp.Items) != 2 {
		t.Fatalf("webhook events returned = %d, want 2 (both projects' events, ungated by grants)", len(logsResp.Items))
	}
}

// TestReadVisibility_ReadingDoesNotImplyActing pins the other half of
// design.md D6: global read visibility never extends to write, run, or
// lifecycle actions, which stay governed by the effective role for the
// specific project.
func TestReadVisibility_ReadingDoesNotImplyActing(t *testing.T) {
	srv := NewServer(Config{})
	projectStore := srv.projectStore.(*mocks.MockProjectStore)
	projectStore.Projects = map[string]*domain.ProjectConfig{
		"alpha": {Name: "alpha", RepoURL: "https://git.test/alpha.git", RepoRef: "main"},
	}
	token := authToken(t, srv, domain.RoleViewer) // can read alpha, holds no operator grant on it

	req := httptest.NewRequest(http.MethodPost, "/api/projects/alpha/run", nil)
	req.Header.Set("Authorization", token)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (read visibility must not authorize starting a run)", w.Code)
	}
}
