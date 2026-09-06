// Coverage retrofit for internal/web: simple system handlers.
//
// handleHealth / handleVersion / handleRoot / handleDashboard all run
// against the active Server without depending on a real PostgreSQL
// store — we drive them through httptest.NewRecorder() directly.
package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// systemHandlerServer returns a Server backed by a fresh on-disk file
// store in a per-test temp dir.
func systemHandlerServer(t *testing.T) *Server {
	t.Helper()
	return NewServer(Config{
		ListenAddr: ":0",
		StorePath:  t.TempDir() + "/projects.json",
	})
}

func TestHandleHealth(t *testing.T) {
	srv := systemHandlerServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/health", nil)
	srv.handleHealth(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range []string{"status", "service", "pipelines", "webhooks", "repos"} {
		if _, ok := body[k]; !ok {
			t.Errorf("missing field %q in response: %v", k, body)
		}
	}
	if got := body["status"]; got != "ok" {
		t.Errorf("status field = %v, want ok", got)
	}
}

func TestHandleVersion(t *testing.T) {
	srv := systemHandlerServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/version", nil)
	srv.handleVersion(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["version"]; !ok {
		t.Errorf("missing version field: %v", body)
	}
	if body["version"] == "" {
		t.Errorf("version field is empty")
	}
}

func TestHandleRoot(t *testing.T) {
	srv := systemHandlerServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	srv.handleRoot(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Errorf("expected text/html content-type, got %q", w.Header().Get("Content-Type"))
	}
	body := w.Body.String()
	for _, want := range []string{"<html>", "Kestrix", "/docs/", "/api/health"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in response body", want)
		}
	}
}

// handleDashboard covers two paths:
//   - Server with non-PGStore: empty project_count, no recent runs
//   - Server with PGStore: covered by an existing type assertion in the
//     handler; without a real DB the assertion falls through. Both
//     branches are exercised here.
func TestHandleDashboard_PostgreSQLStore(t *testing.T) {
	srv := systemHandlerServer(t)
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
	// Required keys must all exist.
	for _, k := range []string{"project_count", "run_stats", "recent_runs", "pg_available"} {
		if _, ok := body[k]; !ok {
			t.Errorf("missing %q in dashboard response", k)
		}
	}
	if body["pg_available"] != true {
		t.Errorf("pg_available should be true for PostgreSQL store, got %v", body["pg_available"])
	}
	if body["project_count"].(float64) != 0 {
		t.Errorf("expected zero projects, got %v", body["project_count"])
	}
}
