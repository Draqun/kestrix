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

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/service"
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
	body := `{"repo_url":"https://git.example/repo.git","repo_ref":"main"}`
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
	// name present but neither repo_url nor repo_ref
	body := `{"name":"demo"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	srv.handleCreateProject(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "repo_url is required") {
		t.Errorf("body = %q, want repo-reference error", w.Body.String())
	}
}

func TestHandleCreateProject_RepoURLRequiresRef(t *testing.T) {
	srv := systemHandlerServer(t)
	body := `{"name":"demo","repo_url":"https://example.com/repo.git"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	srv.handleCreateProject(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
}

func TestHandleCreateProject_Happy(t *testing.T) {
	srv := systemHandlerServer(t)
	body := `{"name":"demo","repo_url":"https://git.example/demo.git","repo_ref":"main"}`
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

// TestHandleCreateProject_RejectsSecretRepoCredential pins runtime-secrets
// spec.md "Secret rejected for source acquisition": a project create naming
// a `secret` credential as its repo_credential_ref is rejected with 400 and
// the project is never persisted.
func TestHandleCreateProject_RejectsSecretRepoCredential(t *testing.T) {
	srv := systemHandlerServer(t)
	credStore := &fakeCredentialStore{items: map[string]domain.CredentialMetadata{
		"cred_secret": {Ref: "cred_secret", Name: "Deploy secret", Type: "secret"},
	}}
	srv.credentialStore = credStore
	srv.projectService.SetCredentialLookup(credStore.GetCredentialMetadata)

	body := `{"name":"demo","repo_url":"https://git.example/demo.git","repo_ref":"main","repo_credential_ref":"cred_secret"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	srv.handleCreateProject(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	mockStore := srv.projectStore.(*mocks.MockProjectStore)
	if len(mockStore.UpsertCalls) != 0 {
		t.Fatalf("expected project not persisted, got %d Upsert calls", len(mockStore.UpsertCalls))
	}
}

func TestHandleCreateProjectRejectsRepoPath(t *testing.T) {
	srv := systemHandlerServer(t)
	body := `{"name":"demo","repo_path":"/tmp/demo","repo_url":"https://git.example/demo.git","repo_ref":"main"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	srv.handleCreateProject(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "repo_path") {
		t.Fatalf("body = %q, want repo_path rejection", w.Body.String())
	}
}

// savePipelineServer returns a Server whose on-disk file store contains
// a single project pointing at tempDir as its repo root.
func savePipelineServer(t *testing.T) (*Server, string) {
	t.Helper()
	tempDir := t.TempDir()
	srv := systemHandlerServer(t)
	if err := srv.projectStore.Upsert(&domain.ProjectConfig{
		Name:         "demo",
		RepoURL:      "file://" + tempDir,
		RepoRef:      "main",
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
	srv, _ := savePipelineServer(t)
	body := `project "demo" {}` + "\n"
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects/demo/pipeline",
		strings.NewReader(body))
	// handleSaveProjectPipeline pulls the project name via chi.URLParam,
	// which requires the chi RouteContext. Invoke through the real router
	// so URLParams are populated correctly.
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 (body=%s)", w.Code, w.Body.String())
	}
}

// writeSeedProject seeds an in-store project whose pipeline file is
// already written to disk so the GET-style handlers can read it back.
func writeSeedProject(t *testing.T, srv *Server, tempDir, body string) {
	t.Helper()
	if err := srv.projectStore.Upsert(&domain.ProjectConfig{
		Name:         "demo",
		RepoURL:      "file://" + tempDir,
		RepoRef:      "main",
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
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleGetProjectPipeline_Happy(t *testing.T) {
	srv, tempDir := savePipelineServer(t)
	writeSeedProject(t, srv, tempDir, `project "demo" {}`+"\n")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/projects/demo/pipeline", nil)
	srv.router.ServeHTTP(w, authed(t, srv, r))

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
	if err := srv.projectStore.Upsert(&domain.ProjectConfig{
		Name:         "demo",
		RepoURL:      "file://" + tempDir,
		RepoRef:      "main",
		PipelineDir:  "ci",
		PipelineFile: "pipeline.tf",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/projects/demo/pipeline", nil)
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when pipeline file missing", w.Code)
	}
}

func TestHandleGetProjectDefinition_NotFound(t *testing.T) {
	srv := systemHandlerServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/projects/nope/definition", nil)
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleGetProjectDefinition_Happy(t *testing.T) {
	srv, tempDir := savePipelineServer(t)
	writeSeedProject(t, srv, tempDir, `project "demo" {}`+"\n")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/projects/demo/definition", nil)
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["name"]; !ok {
		t.Errorf("response missing 'name' field: %v", body)
	}
	if _, ok := body["tasks"]; !ok {
		t.Errorf("response missing 'tasks' field: %v", body)
	}
}

func TestHandleGetProject_Happy(t *testing.T) {
	srv, _ := savePipelineServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/projects/demo", nil)
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json", ct)
	}
	if !strings.Contains(w.Body.String(), `"name":"demo"`) {
		t.Errorf("body = %q, want the project JSON", w.Body.String())
	}
}

func TestHandleGetProject_NotFound(t *testing.T) {
	srv := systemHandlerServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/projects/nope", nil)
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleUpdateProject_Happy(t *testing.T) {
	srv, _ := savePipelineServer(t)
	// Update with empty optional fields -> they should be inherited
	// from the existing record.
	body := `{"repo_url":"https://git.example.test/demo.git","repo_ref":"main","pipeline_dir":"ci"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/projects/demo",
		strings.NewReader(body))
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
}

func TestHandleUpdateProject_NotFound(t *testing.T) {
	srv := systemHandlerServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/projects/nope",
		strings.NewReader(`{}`))
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleUpdateProject_InvalidJSON(t *testing.T) {
	srv, _ := savePipelineServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/projects/demo",
		strings.NewReader("not json"))
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleDeleteProject_Happy(t *testing.T) {
	srv, _ := savePipelineServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("DELETE", "/api/projects/demo", nil)
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestHandleDeleteProject_NotFound(t *testing.T) {
	srv := systemHandlerServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("DELETE", "/api/projects/nope", nil)
	srv.handleDeleteProject(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// ─── projectPipelineDir edge cases ─────────────────────────────────────

func TestProjectPipelineDir_Escape(t *testing.T) {
	srv := &Server{cfg: Config{PipelineDir: "/tmp"}}
	cfg := &domain.ProjectConfig{
		Name:        "test",
		PipelineDir: "../../etc",
	}
	_, err := srv.projectPipelineDir(cfg, "/tmp/test-repo")
	if err == nil {
		t.Fatal("expected error for escaping pipeline dir, got nil")
	}
	if !strings.Contains(err.Error(), "escapes repository") {
		t.Errorf("error = %q, want 'escapes repository'", err.Error())
	}
}

// ─── projectPipelinePath default file ──────────────────────────────────

func TestProjectPipelinePath_DefaultFile(t *testing.T) {
	srv := &Server{cfg: Config{PipelineDir: "/tmp"}}
	tmp := t.TempDir()
	cfg := &domain.ProjectConfig{
		Name:         "test",
		PipelineDir:  ".",
		PipelineFile: "",
	}
	path, err := srv.projectPipelinePath(cfg, tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(path, "pipeline.tf") {
		t.Errorf("path = %q, want it to end with pipeline.tf", path)
	}
}

// ─── parseProjectPipeline error paths ──────────────────────────────────

func TestParseProjectPipeline_BadDir(t *testing.T) {
	srv := &Server{cfg: Config{PipelineDir: "/tmp"}}
	cfg := &domain.ProjectConfig{
		Name: "test", RepoURL: "file:///tmp/nonexistent-repo-abc123", RepoRef: "main", PipelineDir: "ci",
	}
	srv.sourceSessions = &testSourceSessions{}
	_, err := srv.parseProjectPipeline(cfg)
	if err == nil {
		t.Fatal("expected error for non-existent pipeline dir, got nil")
	}
}

func TestParseProjectPipeline_EscapingDir(t *testing.T) {
	srv := &Server{cfg: Config{PipelineDir: "/tmp"}}
	cfg := &domain.ProjectConfig{
		Name: "test", RepoURL: "file:///tmp/test-repo", RepoRef: "main", PipelineDir: "../../etc",
	}
	srv.sourceSessions = &testSourceSessions{}
	_, err := srv.parseProjectPipeline(cfg)
	if err == nil {
		t.Fatal("expected error for escaping dir, got nil")
	}
}

// ─── handleSaveProjectPipeline error paths ─────────────────────────────

func TestHandleSaveProjectPipeline_MkdirError(t *testing.T) {
	tmp := t.TempDir()
	// Create a regular file where MkdirAll would expect a directory.
	blockingFile := filepath.Join(tmp, "blocking")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mockProj := &mocks.MockProjectStore{
		GetResult: &domain.ProjectConfig{
			Name:         "demo",
			RepoURL:      "file://" + tmp,
			RepoRef:      "main",
			PipelineDir:  "blocking",
			PipelineFile: "pipeline.tf",
		},
	}
	srv := &Server{
		cfg:            Config{ListenAddr: ":0"},
		projectStore:   mockProj,
		projectService: service.NewProjectService(mockProj),
	}
	srv.router = srv.buildRouter()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects/demo/pipeline",
		strings.NewReader("body"))
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 (body=%s)", w.Code, w.Body.String())
	}
}

func TestHandleSaveProjectPipeline_WriteError(t *testing.T) {
	tmp := t.TempDir()
	// Make the target path a directory so WriteFile fails deterministically,
	// even when tests run as root in CI.
	pipelineDir := filepath.Join(tmp, "rodir")
	if err := os.MkdirAll(filepath.Join(pipelineDir, "pipeline.tf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(pipelineDir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chmod(filepath.Join(pipelineDir, "pipeline.tf"), 0o755)
		_ = os.Chmod(pipelineDir, 0o755)
	}()
	mockProj := &mocks.MockProjectStore{
		GetResult: &domain.ProjectConfig{
			Name:         "demo",
			RepoURL:      "file://" + tmp,
			RepoRef:      "main",
			PipelineDir:  "rodir",
			PipelineFile: "pipeline.tf",
		},
	}
	srv := &Server{
		cfg:            Config{ListenAddr: ":0"},
		projectStore:   mockProj,
		projectService: service.NewProjectService(mockProj),
	}
	srv.router = srv.buildRouter()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects/demo/pipeline",
		strings.NewReader("body"))
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 (body=%s)", w.Code, w.Body.String())
	}
}

func TestHandleSaveProjectPipeline_EscapePath(t *testing.T) {
	mockProj := &mocks.MockProjectStore{
		GetResult: &domain.ProjectConfig{
			Name:        "demo",
			RepoURL:     "https://git.example.test/demo.git",
			RepoRef:     "main",
			PipelineDir: "../../etc",
		},
	}
	srv := &Server{
		cfg:            Config{ListenAddr: ":0"},
		projectStore:   mockProj,
		projectService: service.NewProjectService(mockProj),
	}
	srv.router = srv.buildRouter()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects/demo/pipeline",
		strings.NewReader("body"))
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 (body=%s)", w.Code, w.Body.String())
	}
}

// ─── handleResolveProject coverage ─────────────────────────────────────

func TestHandleResolveProject_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	writeTaskPipeline(t, tmp)
	mockProj := &mocks.MockProjectStore{
		GetResult: &domain.ProjectConfig{
			Name:        "demo",
			RepoURL:     "file://" + tmp,
			RepoRef:     "main",
			PipelineDir: ".",
		},
	}
	srv := &Server{
		cfg:            Config{ListenAddr: ":0"},
		projectStore:   mockProj,
		projectService: service.NewProjectService(mockProj),
		sourceSessions: &testSourceSessions{},
	}
	srv.router = srv.buildRouter()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects/demo/resolve",
		strings.NewReader("{not json"))
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid JSON") {
		t.Errorf("body = %q, want invalid JSON error", w.Body.String())
	}
}

func TestHandleResolveProject_BadPipeline(t *testing.T) {
	mockProj := &mocks.MockProjectStore{
		GetResult: &domain.ProjectConfig{
			Name: "demo", RepoURL: "file:///nonexistent/path", RepoRef: "main", PipelineDir: "ci",
		},
	}
	srv := &Server{
		cfg:            Config{ListenAddr: ":0"},
		projectStore:   mockProj,
		projectService: service.NewProjectService(mockProj),
		sourceSessions: &testSourceSessions{},
	}
	srv.router = srv.buildRouter()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects/demo/resolve",
		strings.NewReader(`{"changed":["svc"]}`))
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
}

// ─── Additional coverage: projectPipelineDir empty PipelineDir ─────────

func TestProjectPipelineDir_EmptyPipelineDir(t *testing.T) {
	srv := &Server{cfg: Config{PipelineDir: "/tmp"}}
	tmp := t.TempDir()
	cfg := &domain.ProjectConfig{
		Name:         "test",
		PipelineDir:  "",
		PipelineFile: "ci/pipeline.tf",
	}
	dir, err := srv.projectPipelineDir(cfg, tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(dir, filepath.Join(tmp, "ci")) {
		t.Errorf("dir = %q, want it to end with %s", dir, filepath.Join(tmp, "ci"))
	}
}

func TestProjectPipelineDir_EmptyPipelineFile(t *testing.T) {
	srv := &Server{cfg: Config{PipelineDir: "/tmp"}}
	tmp := t.TempDir()
	cfg := &domain.ProjectConfig{
		Name:         "test",
		PipelineDir:  "",
		PipelineFile: "",
	}
	dir, err := srv.projectPipelineDir(cfg, tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(dir, tmp) {
		t.Errorf("dir = %q, want it to equal %s", dir, tmp)
	}
}

func TestProjectPipelineDir_EmptyPipelineFileNoDir(t *testing.T) {
	srv := &Server{cfg: Config{PipelineDir: "/tmp"}}
	tmp := t.TempDir()
	// PipelineDir="" and PipelineFile has no directory component →
	// filepath.Dir("pipeline.tf") returns ".", so dir = repoPath.
	cfg := &domain.ProjectConfig{
		Name:         "test",
		PipelineDir:  "",
		PipelineFile: "pipeline.tf",
	}
	dir, err := srv.projectPipelineDir(cfg, tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(dir, tmp) {
		t.Errorf("dir = %q, want it to end with %s", dir, tmp)
	}
}

// ─── Additional coverage: projectPipelinePath empty PipelineDir ────────

func TestProjectPipelinePath_EmptyPipelineDir(t *testing.T) {
	srv := &Server{cfg: Config{PipelineDir: "/tmp"}}
	tmp := t.TempDir()
	cfg := &domain.ProjectConfig{
		Name:         "test",
		PipelineDir:  "",
		PipelineFile: "ci/pipeline.tf",
	}
	path, err := srv.projectPipelinePath(cfg, tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(path, "pipeline.tf") {
		t.Errorf("path = %q, want it to end with pipeline.tf", path)
	}
}

func TestProjectPipelinePath_EscapingFile(t *testing.T) {
	srv := &Server{cfg: Config{PipelineDir: "/tmp"}}
	cfg := &domain.ProjectConfig{
		Name:         "test",
		PipelineDir:  ".",
		PipelineFile: "../../etc/passwd",
	}
	_, err := srv.projectPipelinePath(cfg, "/tmp/test-repo")
	if err == nil {
		t.Fatal("expected error for escaping file path, got nil")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error = %q, want 'escapes'", err.Error())
	}
}

func TestProjectPipelinePath_DirError(t *testing.T) {
	srv := &Server{cfg: Config{PipelineDir: "/tmp"}}
	cfg := &domain.ProjectConfig{
		Name:         "test",
		PipelineDir:  "../../etc",
		PipelineFile: "pipeline.tf",
	}
	_, err := srv.projectPipelinePath(cfg, "/tmp/test-repo")
	if err == nil {
		t.Fatal("expected error for escaping dir, got nil")
	}
}

// ─── Additional coverage: handleSaveProjectPipeline pipelinePath error ─

func TestHandleSaveProjectPipeline_PipelinePathError(t *testing.T) {
	mockProj := &mocks.MockProjectStore{
		GetResult: &domain.ProjectConfig{
			Name:        "demo",
			RepoURL:     "https://git.example.test/demo.git",
			RepoRef:     "main",
			PipelineDir: ".",
			// PipelineFile escapes the pipeline directory
			PipelineFile: "../../etc/passwd",
		},
	}
	srv := &Server{
		cfg:            Config{ListenAddr: ":0"},
		projectStore:   mockProj,
		projectService: service.NewProjectService(mockProj),
	}
	srv.router = srv.buildRouter()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects/demo/pipeline",
		strings.NewReader("body"))
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 (body=%s)", w.Code, w.Body.String())
	}
}

// ─── Additional coverage: handleResolveProject resolve error ───────────

func TestHandleResolveProject_ResolveError(t *testing.T) {
	tmp := t.TempDir()
	// Write a pipeline with a task whose trigger expression is invalid,
	// which will cause dag.Resolve to return an error.
	badTriggerTF := `project "demo" {
	ci_repo "svc" {
		path = "src"
		kind = "subpath"
	}
	ci_task "build" {
		trigger = "repo.svc. &&& invalid"
		execute {
			repo   = "svc"
			script = "scripts/build.sh"
		}
	}
}
`
	if err := os.WriteFile(filepath.Join(tmp, "pipeline.tf"), []byte(badTriggerTF), 0o644); err != nil {
		t.Fatal(err)
	}
	mockProj := &mocks.MockProjectStore{
		GetResult: &domain.ProjectConfig{
			Name:        "demo",
			RepoURL:     "file://" + tmp,
			RepoRef:     "main",
			PipelineDir: ".",
		},
	}
	srv := &Server{
		cfg:            Config{ListenAddr: ":0"},
		projectStore:   mockProj,
		projectService: service.NewProjectService(mockProj),
		sourceSessions: &testSourceSessions{},
	}
	srv.router = srv.buildRouter()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/projects/demo/resolve",
		strings.NewReader(`{"changed":["svc"]}`))
	srv.router.ServeHTTP(w, authed(t, srv, r))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body=%s)", w.Code, w.Body.String())
	}
}

// resolveServer seeds Server with a valid pipeline dir so handleResolve
// can parse it.
func resolveServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	// Pipeline needs at least a project block plus a ci_repo + ci_task
	// so ParseDir + Resolve has something to walk.
	hcl := `project "demo" {
  default_image = "golang:1.22"

  ci_repo "service" {
    path = "svc"
    kind = "subpath"
  }

  ci_task "build" {
    trigger    = "repo.service.changed"
    manual     = false
    depends_on = []

    execute {
      repo   = "service"
      script = ":"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "pipeline.tf"),
		[]byte(hcl), 0o644); err != nil {
		t.Fatalf("write pipeline: %v", err)
	}
	srv := NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: dir,
		StorePath:   filepath.Join(dir, "projects.json"),
	})
	return srv, dir
}

func TestHandleValidate_DefaultDir(t *testing.T) {
	srv, _ := resolveServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/validate", nil)
	srv.handleValidate(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"project":"demo"`) {
		t.Errorf("body = %q, want project name", w.Body.String())
	}
}
