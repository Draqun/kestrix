package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBoundPipelineDir_Root, _Child, _ParentEscape, ..., together cover every
// case design.md D5 names for boundPipelineDir: the root itself, a
// descendant, a ".." escape, an absolute path elsewhere, a symlink pointing
// out of the root, and a path that does not exist.
func TestBoundPipelineDir_EmptyMeansRootUnresolved(t *testing.T) {
	root := t.TempDir()
	got, err := boundPipelineDir(root, "")
	if err != nil {
		t.Fatalf("boundPipelineDir: %v", err)
	}
	if got != root {
		t.Fatalf("got = %q, want root %q unresolved", got, root)
	}
}

func TestBoundPipelineDir_RootItself(t *testing.T) {
	root := t.TempDir()
	if _, err := boundPipelineDir(root, root); err != nil {
		t.Fatalf("boundPipelineDir(root, root) error = %v, want nil", err)
	}
}

func TestBoundPipelineDir_Child(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "sub")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := boundPipelineDir(root, child)
	if err != nil {
		t.Fatalf("boundPipelineDir: %v", err)
	}
	// boundPipelineDir resolves symlinks; on macOS t.TempDir() lives under
	// /var -> /private/var, so compare against the resolved path.
	if child, err = filepath.EvalSymlinks(child); err != nil {
		t.Fatal(err)
	}
	if got != child {
		t.Fatalf("got = %q, want %q", got, child)
	}
}

func TestBoundPipelineDir_ParentEscapeViaDotDot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	escaped := filepath.Join(sub, "..", "..")
	if _, err := boundPipelineDir(root, escaped); !errors.Is(err, errPipelineDirOutsideRoot) {
		t.Fatalf("boundPipelineDir(%q) error = %v, want errPipelineDirOutsideRoot", escaped, err)
	}
}

func TestBoundPipelineDir_AbsolutePathElsewhere(t *testing.T) {
	root := t.TempDir()
	elsewhere := t.TempDir()
	if root == elsewhere {
		t.Fatal("t.TempDir() returned the same path twice")
	}
	if _, err := boundPipelineDir(root, elsewhere); !errors.Is(err, errPipelineDirOutsideRoot) {
		t.Fatalf("boundPipelineDir(elsewhere) error = %v, want errPipelineDirOutsideRoot", err)
	}
}

func TestBoundPipelineDir_SymlinkEscapesRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if _, err := boundPipelineDir(root, link); !errors.Is(err, errPipelineDirOutsideRoot) {
		t.Fatalf("boundPipelineDir(symlink out of root) error = %v, want errPipelineDirOutsideRoot", err)
	}
}

func TestBoundPipelineDir_SymlinkInsideRootIsAllowed(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "alias")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	got, err := boundPipelineDir(root, link)
	if err != nil {
		t.Fatalf("boundPipelineDir(in-root symlink): %v", err)
	}
	resolvedTarget, err := resolvePipelinePath(target)
	if err != nil {
		t.Fatal(err)
	}
	if got != resolvedTarget {
		t.Fatalf("got = %q, want resolved target %q", got, resolvedTarget)
	}
}

func TestBoundPipelineDir_NonexistentPath(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "does-not-exist")
	if _, err := boundPipelineDir(root, missing); !errors.Is(err, errPipelineDirOutsideRoot) {
		t.Fatalf("boundPipelineDir(missing) error = %v, want errPipelineDirOutsideRoot", err)
	}
}

// TestBoundPipelineDir_OutsideAndMissingAnswerIdentically pins design.md
// D5's "not a path oracle" property at the function level: a path outside
// the root and a path that simply does not exist must be indistinguishable
// to the caller.
func TestBoundPipelineDir_OutsideAndMissingAnswerIdentically(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	missing := filepath.Join(root, "does-not-exist")

	_, outsideErr := boundPipelineDir(root, outside)
	_, missingErr := boundPipelineDir(root, missing)
	if outsideErr == nil || missingErr == nil {
		t.Fatalf("both must error: outside=%v missing=%v", outsideErr, missingErr)
	}
	if outsideErr.Error() != missingErr.Error() {
		t.Fatalf("errors differ: outside=%q missing=%q, want identical", outsideErr, missingErr)
	}
}

// TestResolvePipelinePath_AbsFailurePropagates covers filepath.Abs's one
// failure mode: the process's current directory no longer exists, so a
// relative path cannot be resolved. It removes the test's working
// directory out from under it - Getwd's own tests use this technique - and
// restores a valid one immediately after via t.Cleanup.
func TestResolvePipelinePath_AbsFailurePropagates(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	removed := t.TempDir()
	if err := os.Chdir(removed); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	if err := os.RemoveAll(removed); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	if _, err := resolvePipelinePath("relative-path"); err == nil {
		t.Fatal("resolvePipelinePath() error = nil, want an error once the working directory is gone")
	}
}

func TestBoundPipelineDir_UnusableRootIsRefused(t *testing.T) {
	root := filepath.Join(t.TempDir(), "does-not-exist-either")
	if _, err := boundPipelineDir(root, filepath.Join(root, "sub")); !errors.Is(err, errPipelineDirOutsideRoot) {
		t.Fatalf("boundPipelineDir with an unusable root error = %v, want errPipelineDirOutsideRoot", err)
	}
}

// TestPipelineRoutes_OutsideRootDirRejected drives all five routes that
// accept a caller-supplied pipeline directory (handler_pipeline.go) through
// the real router with a directory outside the configured pipeline root,
// and asserts each answers 403 without reaching the parser; see design.md
// D5, task 4.2.
func TestPipelineRoutes_OutsideRootDirRejected(t *testing.T) {
	root := t.TempDir()
	writeTaskPipeline(t, root)
	outside := t.TempDir()
	srv := NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: root,
		StorePath:   filepath.Join(root, "projects.json"),
	})

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"GetPipeline", http.MethodGet, "/api/pipeline?dir=" + outside, ""},
		{"GetPipelineRaw", http.MethodGet, "/api/pipeline/raw?dir=" + outside, ""},
		{"Resolve", http.MethodPost, "/api/resolve", `{"dir":"` + outside + `"}`},
		{"Generate", http.MethodPost, "/api/generate", `{"dir":"` + outside + `"}`},
		{"Validate", http.MethodGet, "/api/validate?dir=" + outside, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body *strings.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			} else {
				body = strings.NewReader("")
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			req = authed(t, srv, req)
			w := httptest.NewRecorder()
			srv.router.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), outside) {
				t.Fatalf("body = %q must not echo the requested path", w.Body.String())
			}
		})
	}
}

// TestPipelineRoutes_OmittedDirUsesConfiguredRoot asserts every one of the
// five routes still defaults to s.cfg.PipelineDir, unresolved, when dir is
// omitted - the behavior boundPipelineDir must leave unchanged.
func TestPipelineRoutes_OmittedDirUsesConfiguredRoot(t *testing.T) {
	root := t.TempDir()
	writeTaskPipeline(t, root)
	srv := NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: root,
		StorePath:   filepath.Join(root, "projects.json"),
	})

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"GetPipeline", http.MethodGet, "/api/pipeline", ""},
		{"GetPipelineRaw", http.MethodGet, "/api/pipeline/raw", ""},
		{"Resolve", http.MethodPost, "/api/resolve", `{}`},
		{"Generate", http.MethodPost, "/api/generate", `{}`},
		{"Validate", http.MethodGet, "/api/validate", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.NewReader(tc.body)
			req := httptest.NewRequest(tc.method, tc.path, body)
			req = authed(t, srv, req)
			w := httptest.NewRecorder()
			srv.router.ServeHTTP(w, req)
			if w.Code == http.StatusForbidden {
				t.Fatalf("status = 403 with dir omitted, want the configured root to be used (body=%s)", w.Body.String())
			}
		})
	}
}
