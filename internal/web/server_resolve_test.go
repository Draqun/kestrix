// Coverage for internal/web: resolve, generate, validate, and logs
// handlers. These don't depend on PostgreSQL — they use parser.ParseDir
// and dag.New, which work with file-based pipelines.
package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pipelineWithTaskTF is a minimal HCL pipeline with one ci_repo and one
// ci_task so resolve/generate/validate have something to operate on.
const pipelineWithTaskTF = `project "demo" {
	ci_repo "svc" {
		path = "src"
		kind = "subpath"
	}
	ci_task "build" {
		trigger = "repo.svc.changed"
		execute {
			repo   = "svc"
			script = "scripts/build.sh"
		}
	}
}
`

func writeTaskPipeline(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "pipeline.tf"), []byte(pipelineWithTaskTF), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- handleResolve --------------------------------------------------------

func TestHandleResolve_Happy(t *testing.T) {
	dir := t.TempDir()
	writeTaskPipeline(t, dir)
	srv := NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: dir,
		StorePath:   filepath.Join(dir, "projects.json"),
	})
	body := `{"changed":["svc"],"dir":"` + dir + `"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/resolve", strings.NewReader(body))
	srv.handleResolve(w, r)

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
}

func TestHandleResolve_InvalidJSON(t *testing.T) {
	srv := systemHandlerServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/resolve", strings.NewReader("{not json"))
	srv.handleResolve(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid JSON") {
		t.Errorf("body = %q, want invalid JSON error", w.Body.String())
	}
}

func TestHandleResolve_BadDir(t *testing.T) {
	srv := systemHandlerServer(t)
	body := `{"dir":"/nonexistent/path"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/resolve", strings.NewReader(body))
	srv.handleResolve(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// --- handleGenerate --------------------------------------------------------

func TestHandleGenerate_Happy(t *testing.T) {
	dir := t.TempDir()
	writeTaskPipeline(t, dir)
	srv := NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: dir,
		StorePath:   filepath.Join(dir, "projects.json"),
	})
	body := `{"changed":["svc"],"dir":"` + dir + `"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/generate", strings.NewReader(body))
	srv.handleGenerate(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["yaml"]; !ok {
		t.Errorf("missing 'yaml' field: %v", resp)
	}
}

func TestHandleGenerate_InvalidJSON(t *testing.T) {
	srv := systemHandlerServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/generate", strings.NewReader("{not json"))
	srv.handleGenerate(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleGenerate_BadDir(t *testing.T) {
	srv := systemHandlerServer(t)
	body := `{"dir":"/nonexistent/path"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/generate", strings.NewReader(body))
	srv.handleGenerate(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// --- handleValidate --------------------------------------------------------

func TestHandleValidate_Happy(t *testing.T) {
	dir := t.TempDir()
	writeTaskPipeline(t, dir)
	srv := NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: dir,
		StorePath:   filepath.Join(dir, "projects.json"),
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/validate?dir="+dir, nil)
	srv.handleValidate(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["valid"] != true {
		t.Errorf("valid = %v, want true", resp["valid"])
	}
}

func TestHandleValidate_BadDir(t *testing.T) {
	srv := systemHandlerServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/validate?dir=/nonexistent/path", nil)
	srv.handleValidate(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "error") {
		t.Errorf("body = %q, want error field", w.Body.String())
	}
}

func TestHandleValidate_CycleDetected(t *testing.T) {
	dir := t.TempDir()
	cycleTF := `project "demo" {
	ci_repo "svc" {
		path = "src"
		kind = "subpath"
	}
	ci_task "a" {
		trigger = "repo.svc.changed"
		depends_on = ["b"]
		execute {
			repo   = "svc"
			script = "."
		}
	}
	ci_task "b" {
		trigger = "repo.svc.changed"
		depends_on = ["a"]
		execute {
			repo   = "svc"
			script = "."
		}
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "pipeline.tf"), []byte(cycleTF), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: dir,
		StorePath:   filepath.Join(dir, "projects.json"),
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/validate?dir="+dir, nil)
	srv.handleValidate(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "cycle") {
		t.Errorf("body = %q, want cycle error", w.Body.String())
	}
}

// --- handleGetLogs ---------------------------------------------------------

func TestHandleGetLogs_Empty(t *testing.T) {
	srv := systemHandlerServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/logs", nil)
	srv.handleGetLogs(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var logs []interface{}
	if err := json.NewDecoder(w.Body).Decode(&logs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("logs = %v, want empty", logs)
	}
}

func TestHandleGetLogs_WithEntries(t *testing.T) {
	srv := systemHandlerServer(t)
	srv.webhookLog = append(srv.webhookLog, map[string]interface{}{
		"event": "push", "project": "demo",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/logs", nil)
	srv.handleGetLogs(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var logs []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&logs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(logs) != 1 || logs[0]["event"] != "push" {
		t.Errorf("logs = %v, want one push entry", logs)
	}
}
