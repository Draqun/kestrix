// Tests for project-shaped HTTP handlers in server.go:
// handleCreateProject. The handler covers four failure modes (invalid
// JSON, missing name, missing repo reference, store.Upsert error) plus
// the happy path returning 201 Created with the echoed config.
package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/lunaris-bot/metaci/internal/store"
)

func TestHandleCreateProject_InvalidJSON(t *testing.T) {
	srv := systemHandlerServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects", strings.NewReader("{not json"))
	srv.handleCreateProject(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid JSON") {
		t.Errorf("body = %q, want it to mention invalid JSON", w.Body.String())
	}
}

func TestHandleCreateProject_MissingName(t *testing.T) {
	srv := systemHandlerServer(t)
	// repo_path present but no name
	body := `{"repo_path":"/tmp/repo"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	srv.handleCreateProject(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "name is required") {
		t.Errorf("body = %q, want name-required error", w.Body.String())
	}
}

func TestHandleCreateProject_MissingRepo(t *testing.T) {
	srv := systemHandlerServer(t)
	// name present but neither repo_path nor repo_url
	body := `{"name":"demo"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	srv.handleCreateProject(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "repo_path or repo_url is required") {
		t.Errorf("body = %q, want repo-reference error", w.Body.String())
	}
}

func TestHandleCreateProject_RepoURLAlone(t *testing.T) {
	// repo_url alone (no repo_path) is accepted — exercises the second
	// clause of the OR condition in the guard.
	srv := systemHandlerServer(t)
	body := `{"name":"demo","repo_url":"https://example.com/repo.git"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	srv.handleCreateProject(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
}

func TestHandleCreateProject_Happy(t *testing.T) {
	srv := systemHandlerServer(t)
	body := `{"name":"demo","repo_path":"/tmp/demo"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	srv.handleCreateProject(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json", ct)
	}
	if !strings.Contains(w.Body.String(), `"name":"demo"`) {
		t.Errorf("body = %q, want echoed config", w.Body.String())
	}
}

// savePipelineServer returns a Server whose on-disk file store contains
// a single project pointing at tempDir as its repo root.
func savePipelineServer(t *testing.T) (*Server, string) {
	t.Helper()
	tempDir := t.TempDir()
	srv := systemHandlerServer(t)
	if err := srv.store.Upsert(&store.ProjectConfig{
		Name:         "demo",
		RepoPath:     tempDir,
		PipelineDir:  "ci",
		PipelineFile: "pipeline.tf",
	}); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}
	return srv, tempDir
}

func TestHandleSaveProjectPipeline_ProjectNotFound(t *testing.T) {
	srv := systemHandlerServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects/nope/pipeline",
		strings.NewReader("anything"))
	srv.handleSaveProjectPipeline(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if !strings.Contains(w.Body.String(), "project not found") {
		t.Errorf("body = %q, want project-not-found error", w.Body.String())
	}
}

func TestHandleSaveProjectPipeline_Happy(t *testing.T) {
	srv, tempDir := savePipelineServer(t)
	body := `project "demo" {}` + "\n"
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects/demo/pipeline",
		strings.NewReader(body))
	// handleSaveProjectPipeline pulls the project name via chi.URLParam,
	// which requires the chi RouteContext. Invoke through the real router
	// so URLParams are populated correctly.
	srv.router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "saved" {
		t.Errorf("status field = %v, want saved", resp["status"])
	}
	if path, _ := resp["path"].(string); !strings.Contains(path, "pipeline.tf") {
		t.Errorf("path = %v, want it to point at pipeline.tf", resp["path"])
	}
	// And the file should actually exist on disk with our body.
	saved, err := os.ReadFile(filepath.Join(tempDir, "ci", "pipeline.tf"))
	if err != nil {
		t.Fatalf("expect pipeline.tf on disk: %v", err)
	}
	if !strings.Contains(string(saved), "project \"demo\"") {
		t.Errorf("saved file = %q, want it to contain our body", saved)
	}
}

// writeSeedProject seeds an in-store project whose pipeline file is
// already written to disk so the GET-style handlers can read it back.
func writeSeedProject(t *testing.T, srv *Server, tempDir, body string) {
	t.Helper()
	if err := srv.store.Upsert(&store.ProjectConfig{
		Name:         "demo",
		RepoPath:     tempDir,
		PipelineDir:  "ci",
		PipelineFile: "pipeline.tf",
	}); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tempDir, "ci"), 0o755); err != nil {
		t.Fatalf("mkdir ci: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "ci", "pipeline.tf"),
		[]byte(body), 0o644); err != nil {
		t.Fatalf("write pipeline: %v", err)
	}
}

func TestHandleGetProjectPipeline_NotFound(t *testing.T) {
	srv := systemHandlerServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/projects/nope/pipeline", nil)
	srv.router.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleGetProjectPipeline_Happy(t *testing.T) {
	srv, tempDir := savePipelineServer(t)
	writeSeedProject(t, srv, tempDir, `project "demo" {}`+"\n")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/projects/demo/pipeline", nil)
	srv.router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content-type = %q, want text/plain", ct)
	}
	if !strings.Contains(w.Body.String(), "project \"demo\"") {
		t.Errorf("body = %q, want pipeline content", w.Body.String())
	}
}

func TestHandleGetProjectPipeline_ReadFails(t *testing.T) {
	srv, tempDir := savePipelineServer(t)
	// Seed project points to a file that doesn't exist.
	if err := srv.store.Upsert(&store.ProjectConfig{
		Name:         "demo",
		RepoPath:     tempDir,
		PipelineDir:  "ci",
		PipelineFile: "pipeline.tf",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/projects/demo/pipeline", nil)
	srv.router.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when pipeline file missing", w.Code)
	}
}

func TestHandleGetProjectDefinition_NotFound(t *testing.T) {
	srv := systemHandlerServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/projects/nope/definition", nil)
	srv.router.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleGetProjectDefinition_Happy(t *testing.T) {
	srv, tempDir := savePipelineServer(t)
	writeSeedProject(t, srv, tempDir, `project "demo" {}`+"\n")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/projects/demo/definition", nil)
	srv.router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["Name"]; !ok {
		t.Errorf("response missing 'Name' field: %v", body)
	}
	if _, ok := body["Tasks"]; !ok {
		t.Errorf("response missing 'Tasks' field: %v", body)
	}
}
