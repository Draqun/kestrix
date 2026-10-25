package web

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
)

func postGitHubWebhook(t *testing.T, srv *Server, eventType, body, secret, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/webhook", io.NopCloser(bytes.NewBufferString(body)))
	if eventType != "" {
		req.Header.Set("X-GitHub-Event", eventType)
	}
	if secret != "" {
		req.Header.Set("X-Hub-Signature-256", computeHMAC([]byte(body), secret))
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)
	return rec
}

// 2.2 Handler (D5):
// - ping → 200 {"status":"pong"} after verification, with no history row;
// - other GitHub events → 400;
// - form-encoded GitHub body → 400 naming application/json;
// - unverified ping → 401.

func TestGitHubPing_Success(t *testing.T) {
	srv, _, log, _ := testWebhookServer(t, nil)
	srv.cfg.Signature.Secret = "test-secret"

	body := `{"zen":"Keep it logically awesome.","hook_id":12345}`
	rec := postGitHubWebhook(t, srv, "ping", body, "test-secret", "application/json")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody(t, rec)
	if resp["status"] != "pong" {
		t.Errorf("status = %v, want pong", resp["status"])
	}
	if events := log.List(); len(events) != 0 {
		t.Errorf("ping must not record a webhook event in history, got %d events", len(events))
	}
}

func TestGitHubPing_Unverified(t *testing.T) {
	srv, _, log, _ := testWebhookServer(t, nil)
	srv.cfg.Signature.Secret = "test-secret"

	body := `{"zen":"Keep it logically awesome.","hook_id":12345}`
	// Send without valid secret
	rec := postGitHubWebhook(t, srv, "ping", body, "wrong-secret", "application/json")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
	if events := log.List(); len(events) != 0 {
		t.Errorf("unverified ping must not record a webhook event in history, got %d events", len(events))
	}
}

func TestGitHubUnsupportedEvent(t *testing.T) {
	srv, _, log, _ := testWebhookServer(t, nil)
	srv.cfg.Signature.Secret = "test-secret"

	body := `{"action":"opened","pull_request":{"id":1}}`
	rec := postGitHubWebhook(t, srv, "pull_request", body, "test-secret", "application/json")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody(t, rec)
	errMsg, _ := resp["error"].(string)
	if errMsg == "" {
		t.Errorf("expected error message in response: %v", resp)
	}
	if events := log.List(); len(events) != 0 {
		t.Errorf("unsupported event must not record a webhook event in history, got %d events", len(events))
	}
}

func TestGitHubFormEncodedBody(t *testing.T) {
	srv, _, log, _ := testWebhookServer(t, nil)
	srv.cfg.Signature.Secret = "test-secret"

	body := `payload=%7B%22zen%22%3A%22Keep+it+logically+awesome.%22%7D`
	rec := postGitHubWebhook(t, srv, "ping", body, "test-secret", "application/x-www-form-urlencoded")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody(t, rec)
	errMsg, _ := resp["error"].(string)
	if errMsg != "set the webhook content type to application/json" {
		t.Errorf("error = %q, want 'set the webhook content type to application/json'", errMsg)
	}
	if events := log.List(); len(events) != 0 {
		t.Errorf("form-encoded body rejection must not record a webhook event in history, got %d events", len(events))
	}
}

// 2.3 End-to-end handler test: a signed GitHub push to a registered project's
// repo_ref is recorded with the right changed set and starts a run (via
// webhook-starts-run). A push to a secondary GitHub ci_repo marks it changed.

func TestGitHubPush_PrimaryRepoRefStartsRun(t *testing.T) {
	const pipeline = `project "gh-shop" {
	ci_repo "app" {
		path = "app"
	}
	ci_task "build" {
		trigger = "repo.app.changed"
		execute {
			repo = "app"
			script = ":"
		}
	}
}`
	proj := registeredProject(t, "gh-shop", pipeline, 0)
	dir := strings.TrimPrefix(proj.RepoURL, "file://")
	proj.RepoURL = "https://github.com/org/gh-shop.git"
	proj.Sources = []domain.ProjectSource{
		{Name: "gh-shop", URL: "https://github.com/org/gh-shop.git", Primary: true},
	}
	proj.WebhookSecret = "proj-secret"

	runStore := &mocks.MockRunStore{CreateRunID: 42, FinishRunCh: make(chan struct{}, 1)}
	srv, _, log, sessions := testWebhookServer(t, runStore, proj)
	sessions.root = dir

	body := `{
		"ref": "refs/heads/main",
		"after": "gh111222333",
		"deleted": false,
		"repository": {
			"full_name": "org/gh-shop",
			"default_branch": "main",
			"clone_url": "https://github.com/org/gh-shop.git",
			"ssh_url": "git@github.com:org/gh-shop.git",
			"html_url": "https://github.com/org/gh-shop"
		},
		"commits": [
			{
				"id": "gh111222333",
				"message": "fix app",
				"added": [],
				"modified": ["app/main.go"],
				"removed": []
			}
		]
	}`

	rec := postGitHubWebhook(t, srv, "push", body, "proj-secret", "application/json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody(t, rec)
	if resp["status"] != "ok" {
		t.Fatalf("status = %v, want ok", resp["status"])
	}
	if resp["run_id"] != float64(42) {
		t.Fatalf("run_id = %v, want 42", resp["run_id"])
	}

	// Verify run in store
	if len(runStore.CreateRunCalls) != 1 {
		t.Fatalf("CreateRun calls = %d, want 1", len(runStore.CreateRunCalls))
	}
	call := runStore.CreateRunCalls[0]
	if call.ProjectName != "gh-shop" {
		t.Errorf("project name = %q, want gh-shop", call.ProjectName)
	}
	if call.StartedBy != "webhook:gh-shop" {
		t.Errorf("started_by = %q, want webhook:gh-shop", call.StartedBy)
	}
	if len(call.Changed) != 1 || call.Changed[0] != "app" {
		t.Errorf("changed = %v, want [app]", call.Changed)
	}

	// Verify webhook history row
	events := log.List()
	if len(events) != 1 {
		t.Fatalf("history events count = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.ProjectName != "gh-shop" {
		t.Errorf("history event project = %q, want gh-shop", ev.ProjectName)
	}
	if ev.Status != domain.WebhookStatusTriggered {
		t.Errorf("history event status = %q, want triggered", ev.Status)
	}
	if ev.Revision != "gh111222333" {
		t.Errorf("history event revision = %q, want gh111222333", ev.Revision)
	}
	if len(ev.TriggeredTasks) != 1 || ev.TriggeredTasks[0] != "build" {
		t.Errorf("history event tasks = %v, want [build]", ev.TriggeredTasks)
	}

	<-runStore.FinishRunCh
	sessions.mu.Lock()
	cleaned := sessions.cleaned
	sessions.mu.Unlock()
	if cleaned != 1 {
		t.Errorf("sessions.cleaned = %d, want 1", cleaned)
	}
}

func TestGitHubPush_SecondaryRepoMarksChanged(t *testing.T) {
	const secondaryPipeline = `project "gh-shop" {
	ci_repo "gh_shared" {
		url = "https://github.com/org/gh-shared.git"
		ref = "release"
	}
	ci_task "sync" {
		trigger = "repo.gh_shared.changed"
		execute {
			repo = "gh_shared"
			script = ":"
		}
	}
}`
	proj := registeredProject(t, "gh-shop", secondaryPipeline, 0)
	dir := strings.TrimPrefix(proj.RepoURL, "file://")
	proj.RepoURL = "https://github.com/org/gh-shop.git"
	proj.Sources = []domain.ProjectSource{
		{Name: "gh-shop", URL: "https://github.com/org/gh-shop.git", Primary: true},
		{Name: "gh_shared", URL: "https://github.com/org/gh-shared.git"},
	}
	proj.WebhookSecret = "proj-secret"

	runStore := &mocks.MockRunStore{CreateRunID: 77, FinishRunCh: make(chan struct{}, 1)}
	srv, _, log, sessions := testWebhookServer(t, runStore, proj)
	sessions.root = dir

	body := `{
		"ref": "refs/heads/release",
		"after": "sec999",
		"deleted": false,
		"repository": {
			"full_name": "org/gh-shared",
			"default_branch": "main",
			"clone_url": "https://github.com/org/gh-shared.git",
			"ssh_url": "git@github.com:org/gh-shared.git",
			"html_url": "https://github.com/org/gh-shared"
		},
		"commits": [
			{
				"id": "sec999",
				"message": "shared library update",
				"added": [],
				"modified": ["pkg/util.go"],
				"removed": []
			}
		]
	}`

	rec := postGitHubWebhook(t, srv, "push", body, "proj-secret", "application/json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody(t, rec)
	if resp["run_id"] != float64(77) {
		t.Fatalf("run_id = %v, want 77", resp["run_id"])
	}

	if len(runStore.CreateRunCalls) != 1 {
		t.Fatalf("CreateRun calls = %d, want 1", len(runStore.CreateRunCalls))
	}
	call := runStore.CreateRunCalls[0]
	if call.StartedBy != "webhook:gh_shared" {
		t.Errorf("started_by = %q, want webhook:gh_shared", call.StartedBy)
	}
	if len(call.Changed) != 1 || call.Changed[0] != "gh_shared" {
		t.Errorf("changed = %v, want [gh_shared]", call.Changed)
	}

	events := log.List()
	if len(events) != 1 {
		t.Fatalf("history events count = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.SourceName != "gh_shared" {
		t.Errorf("history event source_name = %q, want gh_shared", ev.SourceName)
	}
	if ev.Revision != "sec999" {
		t.Errorf("history event revision = %q, want sec999", ev.Revision)
	}

	<-runStore.FinishRunCh
	sessions.mu.Lock()
	cleaned := sessions.cleaned
	sessions.mu.Unlock()
	if cleaned != 1 {
		t.Errorf("sessions.cleaned = %d, want 1", cleaned)
	}
}

func TestGitHubPush_UnknownProjectIgnored(t *testing.T) {
	proj := registeredProject(t, "shop", multiSourcePipeline, 0)
	srv, _, log, _ := testWebhookServer(t, nil, proj)
	srv.cfg.Signature.Secret = "global-secret"

	body := `{
		"ref": "refs/heads/main",
		"after": "gh999",
		"deleted": false,
		"repository": {
			"full_name": "org/unknown",
			"default_branch": "main",
			"clone_url": "https://github.com/org/unknown.git"
		},
		"commits": [
			{
				"id": "gh999",
				"message": "some commit",
				"added": ["a.go"],
				"modified": [],
				"removed": []
			}
		]
	}`

	rec := postGitHubWebhook(t, srv, "push", body, "global-secret", "application/json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody(t, rec)
	if resp["status"] != "ignored" || resp["reason"] != "unknown project" {
		t.Errorf("response = %v, want status:ignored, reason:unknown project", resp)
	}
	events := log.List()
	if len(events) != 1 || events[0].Status != domain.WebhookStatusIgnored {
		t.Errorf("history events = %+v, want 1 ignored event", events)
	}
}
