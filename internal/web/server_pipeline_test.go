// Tests for the pipeline-shaped endpoints at the top of server.go:
// handleGetPipeline (line 272) and handleGetPipelineRaw (line 295).
// They share the same query-param-driven "dir" semantics: when the
// request omits "dir" the handler falls back to Server.cfg.PipelineDir.
// We exercise both the success path and each error branch.
package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// emptyDirServer returns a *Server whose PipelineDir points at an
// empty tempdir so handlers that fall back to cfg.PipelineDir don't
// accidentally read a real on-disk pipeline.
func emptyDirServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	return NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: dir,
		StorePath:   filepath.Join(dir, "projects.json"),
	})
}

// writePipeline writes a minimal valid .tf file to <dir>/<name>.tf
// and returns the resulting directory path.
func writePipeline(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// minimalTF is a minimal valid HCL pipeline body. parser.ParseDir is
// permissive enough that any project block with a name is enough to
// test handleGetPipeline's happy path.
const minimalTF = `project "pipe" {
}
`

// TestHandleGetPipeline_Happy covers the 200 response from
// handleGetPipeline when the resolved directory parses cleanly.
func TestHandleGetPipeline_Happy(t *testing.T) {
	dir := t.TempDir()
	writePipeline(t, dir, "p.tf", minimalTF)

	srv := NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: dir,
		StorePath:   filepath.Join(dir, "projects.json"),
	})
	w := httptest.NewRecorder()
	srv.handleGetPipeline(w, httptest.NewRequest("GET", "/api/pipeline?dir="+dir, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content-type = %q, want application/json", got)
	}
	if !strings.Contains(w.Body.String(), "pipe") {
		t.Errorf("body = %q, want project name", w.Body.String())
	}
}

// TestHandleGetPipeline_DirOutsideRoot covers the 403 response boundPipelineDir
// produces for a requested directory outside the configured pipeline root -
// here, one that also happens not to exist, which must answer identically;
// see design.md D5. handleGetPipeline must refuse before the parser ever
// runs.
func TestHandleGetPipeline_DirOutsideRoot(t *testing.T) {
	srv := emptyDirServer(t)
	nonexistent := filepath.Join(t.TempDir(), "no-such-dir")
	w := httptest.NewRecorder()
	srv.handleGetPipeline(w, httptest.NewRequest("GET", "/api/pipeline?dir="+nonexistent, nil))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if !strings.Contains(w.Body.String(), "error") {
		t.Errorf("body = %q, want JSON error", w.Body.String())
	}
	if strings.Contains(w.Body.String(), nonexistent) {
		t.Errorf("body = %q, must not echo the requested path", w.Body.String())
	}
}

// TestHandleGetPipeline_DefaultDir covers the cfg.PipelineDir fallback
// when the request omits the "dir" query parameter.
func TestHandleGetPipeline_DefaultDir(t *testing.T) {
	dir := t.TempDir()
	writePipeline(t, dir, "p.tf", minimalTF)
	srv := NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: dir,
		StorePath:   filepath.Join(dir, "projects.json"),
	})
	w := httptest.NewRecorder()
	srv.handleGetPipeline(w, httptest.NewRequest("GET", "/api/pipeline", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

// TestHandleGetPipelineRaw_Happy covers the 200 + text/plain response
// when the resolved directory contains a single readable .tf file. The
// handler enumerates entries and joins to the first .tf it finds; if
// multiple .tf files exist the handler still picks the first in
// os.ReadDir order, so we keep it simple with one file.
func TestHandleGetPipelineRaw_Happy(t *testing.T) {
	dir := t.TempDir()
	body := minimalTF
	writePipeline(t, dir, "p.tf", body)

	srv := NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: dir,
		StorePath:   filepath.Join(dir, "projects.json"),
	})
	w := httptest.NewRecorder()
	srv.handleGetPipelineRaw(w, httptest.NewRequest("GET", "/api/pipeline/raw?dir="+dir, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("content-type = %q, want text/plain prefix", got)
	}
	if w.Body.String() != body {
		t.Errorf("body mismatch:\ngot=%q\nwant=%q", w.Body.String(), body)
	}
}

// TestHandleGetPipelineRaw_NoTF covers the 404 path when the directory
// exists but contains no .tf file. The handler must return
// {"error":"no .tf file found"} (line 313-315).
func TestHandleGetPipelineRaw_NoTF(t *testing.T) {
	srv := emptyDirServer(t)
	w := httptest.NewRecorder()
	srv.handleGetPipelineRaw(w, httptest.NewRequest("GET", "/api/pipeline/raw", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if !strings.Contains(w.Body.String(), "no .tf file") {
		t.Errorf("body = %q, want 'no .tf file found'", w.Body.String())
	}
}

// TestHandleGetPipelineRaw_DirOutsideRoot covers the 403 boundPipelineDir
// produces for a requested directory outside the configured pipeline root;
// see design.md D5.
func TestHandleGetPipelineRaw_DirOutsideRoot(t *testing.T) {
	srv := emptyDirServer(t)
	nonexistent := filepath.Join(t.TempDir(), "missing")
	w := httptest.NewRecorder()
	srv.handleGetPipelineRaw(w, httptest.NewRequest("GET", "/api/pipeline/raw?dir="+nonexistent, nil))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if !strings.Contains(w.Body.String(), "error") {
		t.Errorf("body = %q, want JSON error", w.Body.String())
	}
}
