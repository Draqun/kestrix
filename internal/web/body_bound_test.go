package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
)

// TestHTTPServerTimeouts pins design.md D5: Config's HTTP timeout fields
// flow into the *http.Server ListenAndServe runs, falling back to the
// package defaults when left zero.
func TestHTTPServerTimeouts(t *testing.T) {
	srv := &Server{cfg: Config{
		ListenAddr:       ":0",
		HTTPReadTimeout:  7 * time.Second,
		HTTPWriteTimeout: 8 * time.Second,
		HTTPIdleTimeout:  9 * time.Second,
	}}
	hs := srv.httpServer()
	if hs.ReadTimeout != 7*time.Second || hs.WriteTimeout != 8*time.Second || hs.IdleTimeout != 9*time.Second {
		t.Fatalf("timeouts = %v/%v/%v, want 7s/8s/9s", hs.ReadTimeout, hs.WriteTimeout, hs.IdleTimeout)
	}

	defaults := (&Server{cfg: Config{ListenAddr: ":0"}}).httpServer()
	if defaults.ReadTimeout != defaultHTTPReadTimeout || defaults.WriteTimeout != defaultHTTPWriteTimeout || defaults.IdleTimeout != defaultHTTPIdleTimeout {
		t.Fatalf("default timeouts = %v/%v/%v, want %v/%v/%v",
			defaults.ReadTimeout, defaults.WriteTimeout, defaults.IdleTimeout,
			defaultHTTPReadTimeout, defaultHTTPWriteTimeout, defaultHTTPIdleTimeout)
	}
}

// TestMaxBodyMiddlewareBoundsAreIndependent pins design.md D5: the /api
// subtree and POST /webhook are bounded by two different, configurable
// values.
func TestMaxBodyMiddlewareBoundsAreIndependent(t *testing.T) {
	srv := NewServer(Config{MaxBodyBytes: 32, WebhookMaxBodyBytes: 4096, Signature: SignatureConfig{Secret: "s"}})

	// A body one byte over the /api bound is refused with 413.
	body := strings.Repeat("a", 33)
	loginBody := `{"name":"` + body + `","password":"x"}`
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(loginBody))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("/api oversized status = %d, want 413 (body=%s)", w.Code, w.Body.String())
	}

	// The same body, well under the webhook bound, reaches handleWebhook -
	// it does not answer 413.
	req = httptest.NewRequest("POST", "/webhook", strings.NewReader(loginBody))
	req.Header.Set("X-Gitlab-Token", "s")
	w = httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code == http.StatusRequestEntityTooLarge {
		t.Fatalf("/webhook status = 413, want the webhook bound (4096) to admit a %d-byte body", len(loginBody))
	}

	// A body over the webhook's own (larger) bound is refused too.
	over := strings.NewReader(strings.Repeat("a", 4097))
	req = httptest.NewRequest("POST", "/webhook", over)
	w = httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("/webhook over its own bound: status = %d, want 413", w.Code)
	}
}

// TestOversizedBodyHasNoBusinessEffect pins task 2.4: every route that
// decodes JSON answers 413 for an oversized body without running the
// route's business logic.
func TestOversizedBodyHasNoBusinessEffect(t *testing.T) {
	srv := NewServer(Config{MaxBodyBytes: 16})
	credStore := &fakeCredentialStore{items: map[string]domain.CredentialMetadata{"cred1": {Ref: "cred1"}}}
	srv.credentialStore = credStore
	projectStore := srv.projectStore.(*mocks.MockProjectStore)
	projectStore.Projects["proj1"] = &domain.ProjectConfig{Name: "proj1", RepoURL: "https://git.example.test/proj1.git", RepoRef: "main"}
	if _, err := srv.userStore.CreateUser("target-user", domain.RoleViewer, "hash"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// A body well over the 16-byte bound for every route below.
	oversized := `{"padding":"` + strings.Repeat("x", 64) + `"}`

	cases := []struct {
		name   string
		method string
		path   string
		public bool
	}{
		{"login", "POST", "/api/auth/login", true},
		{"create credential", "POST", "/api/credentials", false},
		{"rotate credential", "PUT", "/api/credentials/cred1", false},
		{"resolve", "POST", "/api/resolve", false},
		{"generate", "POST", "/api/generate", false},
		{"resolve project", "POST", "/api/projects/proj1/resolve", false},
		{"run project", "POST", "/api/projects/proj1/run", false},
		{"create user", "POST", "/api/users", false},
		{"update user role", "PUT", "/api/users/target-user/role", false},
		{"set user grants", "PUT", "/api/users/target-user/grants", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(oversized))
			if !tc.public {
				req = authed(t, srv, req)
			}
			w := httptest.NewRecorder()
			srv.router.ServeHTTP(w, req)
			if w.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("%s %s: status = %d, want 413 (body=%s)", tc.method, tc.path, w.Code, w.Body.String())
			}
		})
	}

	if len(credStore.items) != 1 {
		t.Errorf("credential store gained an entry: %d items, want 1 (only the seeded one)", len(credStore.items))
	}
	if _, ok := projectStore.Projects["proj1"]; !ok || len(projectStore.Projects) != 1 {
		t.Errorf("project store changed: %+v", projectStore.Projects)
	}
	if len(srv.runStore.(*mocks.MockRunStore).CreateRunCalls) != 0 {
		t.Errorf("a run was started despite the oversized body")
	}
	users, err := srv.userStore.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 { // the seeded target-user plus authed()'s admin
		t.Errorf("user store gained an account: %d users, want 2", len(users))
	}
}
