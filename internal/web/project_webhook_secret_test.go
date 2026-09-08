package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kestrix/internal/domain"
)

// TestPublicProject pins design.md D4: the wire copy carries
// has_webhook_secret and never the secret value, on both a project with a
// secret and one without.
func TestPublicProject(t *testing.T) {
	withSecret := &domain.ProjectConfig{Name: "demo", WebhookSecret: "shh"}
	pub := publicProject(withSecret)
	if !pub.HasWebhookSecret {
		t.Error("HasWebhookSecret = false, want true")
	}
	if pub.WebhookSecret != "" {
		t.Errorf("WebhookSecret = %q, want empty", pub.WebhookSecret)
	}
	if withSecret.WebhookSecret != "shh" {
		t.Errorf("publicProject mutated the original: WebhookSecret = %q", withSecret.WebhookSecret)
	}

	without := &domain.ProjectConfig{Name: "demo"}
	pub = publicProject(without)
	if pub.HasWebhookSecret {
		t.Error("HasWebhookSecret = true for a project with no secret, want false")
	}
}

// TestProjectAPINeverExposesWebhookSecret pins design.md D4 end to end: a
// project created with a webhook_secret never carries webhook_secret or its
// value in the create, list, or detail response, and each reports
// has_webhook_secret:true instead.
func TestProjectAPINeverExposesWebhookSecret(t *testing.T) {
	srv := NewServer(Config{})
	const secret = "top-secret-webhook-value"

	createBody := `{"name":"secretive","repo_url":"https://git.example.test/secretive.git","repo_ref":"main","webhook_secret":"` + secret + `"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects", strings.NewReader(createBody))
	srv.router.ServeHTTP(w, authed(t, srv, r))
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	assertNoWebhookSecret(t, "create", w.Body.String(), secret)

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/projects", nil)
	srv.router.ServeHTTP(w, authed(t, srv, r))
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	assertNoWebhookSecret(t, "list", w.Body.String(), secret)

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/projects/secretive", nil)
	srv.router.ServeHTTP(w, authed(t, srv, r))
	if w.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	assertNoWebhookSecret(t, "detail", w.Body.String(), secret)
}

func assertNoWebhookSecret(t *testing.T, label, body, secretValue string) {
	t.Helper()
	// The quoted key, not the bare substring: "has_webhook_secret" also
	// contains "webhook_secret" and must stay present.
	if strings.Contains(body, `"webhook_secret"`) || strings.Contains(body, `"webhook_secret":`) {
		t.Errorf("%s response carries the webhook_secret field: %s", label, body)
	}
	if strings.Contains(body, secretValue) {
		t.Errorf("%s response carries the secret value: %s", label, body)
	}
	if !strings.Contains(body, `"has_webhook_secret":true`) {
		t.Errorf("%s response missing has_webhook_secret:true: %s", label, body)
	}
}

// TestProjectAPIRejectsInvalidRepoURLBeforeNetworkAccess pins design.md D6 at
// the handler level: POST /api/projects and PUT /api/projects/{name} answer
// 400 for a rejected repo_url, and validateSource (the network / source
// step) is never reached - proven by the source-session manager never
// receiving an Open call.
func TestProjectAPIRejectsInvalidRepoURLBeforeNetworkAccess(t *testing.T) {
	srv := NewServer(Config{})
	sessions := srv.sourceSessions.(*testSourceSessions)

	createBody := `{"name":"bad","repo_url":"http://git.example.test/bad.git","repo_ref":"main"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects", strings.NewReader(createBody))
	srv.router.ServeHTTP(w, authed(t, srv, r))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if len(sessions.requests) != 0 {
		t.Fatalf("create reached the network step: requests = %+v", sessions.requests)
	}

	// Seed a project directly (bypassing the same check) so the update path
	// has something to update.
	if err := srv.projectStore.Upsert(&domain.ProjectConfig{
		Name: "existing", RepoURL: "https://git.example.test/existing.git", RepoRef: "main", CreatedAt: "now",
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	updateBody := `{"repo_url":"file:///etc/passwd","repo_ref":"main"}`
	w = httptest.NewRecorder()
	r = httptest.NewRequest("PUT", "/api/projects/existing", strings.NewReader(updateBody))
	srv.router.ServeHTTP(w, authed(t, srv, r))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("update status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if len(sessions.requests) != 0 {
		t.Fatalf("update reached the network step: requests = %+v", sessions.requests)
	}
}

// TestProjectAPIUpdateWithoutSecretKeepsItAcceptingDeliveries pins design.md
// D4's "Update without a secret keeps the stored one" scenario end to end:
// create with a secret, update the project without sending it, and a
// webhook delivery presenting the original secret still verifies.
func TestProjectAPIUpdateWithoutSecretKeepsItAcceptingDeliveries(t *testing.T) {
	srv := NewServer(Config{})
	const secret = "original-secret"

	createBody := `{"name":"rotator","repo_url":"https://git.example.test/rotator.git","repo_ref":"main","gitlab_project_id":777,"webhook_secret":"` + secret + `"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects", strings.NewReader(createBody))
	srv.router.ServeHTTP(w, authed(t, srv, r))
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}

	// Update omits webhook_secret entirely.
	updateBody := `{"repo_url":"https://git.example.test/rotator.git","repo_ref":"main","gitlab_project_id":777}`
	w = httptest.NewRecorder()
	r = httptest.NewRequest("PUT", "/api/projects/rotator", strings.NewReader(updateBody))
	srv.router.ServeHTTP(w, authed(t, srv, r))
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var updated domain.ProjectConfig
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if !updated.HasWebhookSecret {
		t.Fatal("update response reports has_webhook_secret:false after an omitted update")
	}

	// A webhook delivery presenting the original secret still verifies.
	payload := `{"object_kind":"push","ref":"refs/heads/main","project":{"id":777,"path_with_namespace":"rotator"},"commits":[{"id":"abc","modified":["x.go"]}]}`
	w = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Gitlab-Token", secret)
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("webhook status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
}
