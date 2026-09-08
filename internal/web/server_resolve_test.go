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

	"kestrix/internal/domain"
	"kestrix/internal/web/pagination"
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

func TestHandleResolveRoute_Happy(t *testing.T) {
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

func TestHandleResolveRoute_InvalidJSON(t *testing.T) {
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

// TestHandleResolveRoute_DirOutsideRoot covers the 403 boundPipelineDir
// produces for a requested directory outside the configured pipeline root;
// see design.md D5.
func TestHandleResolveRoute_DirOutsideRoot(t *testing.T) {
	srv := systemHandlerServer(t)
	body := `{"dir":"/nonexistent/path"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/resolve", strings.NewReader(body))
	srv.handleResolve(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

// TestHandleResolveRoute_DirInsideRootButUnparseable covers the pre-existing
// 400 path once boundPipelineDir lets a directory through: the root itself,
// with no pipeline file in it, still reaches parser.ParseDir and fails
// there rather than at the bound check.
func TestHandleResolveRoute_DirInsideRootButUnparseable(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pipeline.tf"), []byte(`project "demo" {`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Config{ListenAddr: ":0", PipelineDir: root, StorePath: filepath.Join(root, "projects.json")})
	body := `{"dir":"` + root + `"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/resolve", strings.NewReader(body))
	srv.handleResolve(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
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

// TestHandleGenerate_DirOutsideRoot covers the 403 boundPipelineDir produces
// for a requested directory outside the configured pipeline root; see
// design.md D5.
func TestHandleGenerate_DirOutsideRoot(t *testing.T) {
	srv := systemHandlerServer(t)
	body := `{"dir":"/nonexistent/path"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/generate", strings.NewReader(body))
	srv.handleGenerate(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

// TestHandleGenerate_DirInsideRootButUnparseable mirrors
// TestHandleResolveRoute_DirInsideRootButUnparseable for handleGenerate.
func TestHandleGenerate_DirInsideRootButUnparseable(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pipeline.tf"), []byte(`project "demo" {`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Config{ListenAddr: ":0", PipelineDir: root, StorePath: filepath.Join(root, "projects.json")})
	body := `{"dir":"` + root + `"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/generate", strings.NewReader(body))
	srv.handleGenerate(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
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

// TestHandleValidate_DirOutsideRoot covers the 403 boundPipelineDir produces
// for a requested directory outside the configured pipeline root; see
// design.md D5.
func TestHandleValidate_DirOutsideRoot(t *testing.T) {
	srv := systemHandlerServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/validate?dir=/nonexistent/path", nil)
	srv.handleValidate(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
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
	var resp struct {
		Items      []domain.WebhookEvent `json:"items"`
		Pagination pagination.Meta       `json:"pagination"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Errorf("items = %v, want empty", resp.Items)
	}
	if resp.Pagination.Total != 0 {
		t.Errorf("total = %d, want 0", resp.Pagination.Total)
	}
	if resp.Items == nil {
		t.Error("items = null, want an empty array")
	}
}

func TestHandleGetLogs_WithEntries(t *testing.T) {
	srv := systemHandlerServer(t)
	log := testWebhookLog(t, srv)
	if err := log.Record(domain.WebhookEvent{
		ProjectName: "demo", Repo: "org/demo", Branch: "main",
		Status: domain.WebhookStatusTriggered, TriggeredTasks: []string{"build"},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/logs", nil)
	srv.handleGetLogs(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Items      []domain.WebhookEvent `json:"items"`
		Pagination pagination.Meta       `json:"pagination"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Repo != "org/demo" {
		t.Fatalf("items = %+v, want one org/demo event", resp.Items)
	}
	if resp.Pagination.Total != 1 {
		t.Errorf("total = %d, want 1", resp.Pagination.Total)
	}
}
