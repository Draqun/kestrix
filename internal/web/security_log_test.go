package web

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kestrix/internal/domain"
)

// captureLog redirects the standard logger to a buffer for the rest of the
// test, restoring it on cleanup. Tests using this must not run with
// t.Parallel(), since the standard logger is a package-level global; no
// test in this package does.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })
	return &buf
}

func TestLogAuthFailure_RecordsRejectedAuthentication(t *testing.T) {
	buf := captureLog(t)
	srv := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	w := httptest.NewRecorder()
	srv.RequireAuth(okHandler()).ServeHTTP(w, req)

	out := buf.String()
	if !strings.Contains(out, "auth:") || !strings.Contains(out, "rejected") {
		t.Fatalf("log output = %q, want a recorded authentication rejection", out)
	}
	if strings.Contains(out, "not-a-real-token") {
		t.Fatalf("log output leaked the token: %q", out)
	}
}

func TestLogAuthFailure_UnknownAccountIsRecorded(t *testing.T) {
	buf := captureLog(t)
	srv := newAuthTestServer(t)
	token, _, err := srv.tokenIssuer.Issue(domain.User{Name: "phantom", Role: domain.RoleViewer, TokenVersion: 1})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.RequireAuth(okHandler()).ServeHTTP(w, req)

	out := buf.String()
	if !strings.Contains(out, "phantom") {
		t.Fatalf("log output = %q, want the account name from the token", out)
	}
	if strings.Contains(out, token) {
		t.Fatalf("log output leaked the token: %q", out)
	}
}

func TestLogDenial_RecordsForbidden(t *testing.T) {
	buf := captureLog(t)
	srv := newAuthTestServer(t)
	req := requestWithClaims(domain.Claims{UserName: "viewer-user", Role: domain.RoleViewer})
	w := httptest.NewRecorder()
	srv.RequireRole(domain.RoleAdmin)(okHandler()).ServeHTTP(w, req)

	out := buf.String()
	if !strings.Contains(out, "authz:") || !strings.Contains(out, "denied") {
		t.Fatalf("log output = %q, want a recorded denial", out)
	}
	if !strings.Contains(out, "viewer-user") {
		t.Fatalf("log output = %q, want the account name", out)
	}
	if !strings.Contains(out, string(domain.RoleAdmin)) || !strings.Contains(out, string(domain.RoleViewer)) {
		t.Fatalf("log output = %q, want both the required and effective roles", out)
	}
}

func TestLogAuthFailure_ThrottledLoginIsRecorded(t *testing.T) {
	buf := captureLog(t)
	clock := &stoppedClock{t: time.Now()}
	srv := newThrottledServer(t, 1, time.Minute, clock)
	body, _ := json.Marshal(loginRequest{Name: "logged-account", Password: "wrong-secret"})
	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	srv.router.ServeHTTP(httptest.NewRecorder(), req1)
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	srv.router.ServeHTTP(httptest.NewRecorder(), req2)

	out := buf.String()
	if !strings.Contains(out, "throttled") {
		t.Fatalf("log output = %q, want a recorded throttle rejection", out)
	}
	if !strings.Contains(out, "logged-account") {
		t.Fatalf("log output = %q, want the attempted account name", out)
	}
	if strings.Contains(out, "wrong-secret") {
		t.Fatalf("log output leaked the submitted password: %q", out)
	}
}
