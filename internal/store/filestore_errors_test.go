// Error-path coverage for internal/store/filestore.
package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pointFileStore builds a *FileStore whose path points at `path`. Bypasses
// New() so we don't pre-create anything on disk.
func pointFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

// nullJSON is the literal JSON `null`. When unmarshalled into a
// map[...]Type it leaves the destination nil, exercising the
// `if projects == nil` branch in loadLocked.
const nullJSON = "null"

// TestDefaultPath_FallbackWhenHomeUnset exercises the UserHomeDir error
// branch in DefaultPath: when HOME is unset, the function falls back to ".".
func TestDefaultPath_FallbackWhenHomeUnset(t *testing.T) {
	t.Setenv("HOME", "")
	got := DefaultPath()
	want := filepath.Join(".", ".metaci", "projects.json")
	if got != want {
		t.Errorf("DefaultPath() with HOME unset = %q, want %q", got, want)
	}
}

// TestUpsert_WriteFileFails covers saveLocked.WriteFile error branch
// reached through Upsert. We pre-create s.path + ".tmp" as a DIRECTORY.
// Since loadLocked succeeds (s.path is a normal file with valid JSON),
// Upsert proceeds to saveLocked, which calls os.WriteFile(tmp, ...) on
// a directory — fails with "is a directory".
func TestUpsert_WriteFileFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.json")
	if err := os.WriteFile(path, []byte(`{"x":{"name":"x","repo_path":"/r"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	st := pointFileStore(path)
	err := st.Upsert(&ProjectConfig{Name: "y", RepoPath: "/r2"})
	if err == nil {
		t.Fatal("expected saveLocked.WriteFile error from .tmp-as-dir")
	}
	if !strings.Contains(err.Error(), "write temp") {
		t.Errorf("expected 'write temp' in error, got %v", err)
	}
}

// TestUpsert_InvalidJSON exercises the loadLocked unmarshal branch
// reached when the on-disk file is not valid JSON.
func TestUpsert_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := pointFileStore(path)
	err := st.Upsert(&ProjectConfig{Name: "foo", RepoPath: "/r"})
	if err == nil {
		t.Fatal("expected loadLocked unmarshal error from invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("expected 'parse' in error, got %v", err)
	}
}

// TestDelete_InvalidJSON exercises loadLocked's unmarshal-error branch
// through Delete.
func TestDelete_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := pointFileStore(path)
	err := st.Delete("anything")
	if err == nil {
		t.Fatal("expected loadLocked unmarshal error from invalid JSON")
	}
}

// TestLoadLocked_ProjectsNilFromJSON covers `if projects == nil` in
// loadLocked reached through Upsert.
func TestLoadLocked_ProjectsNilFromJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nulljson.json")
	if err := os.WriteFile(path, []byte(nullJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	st := pointFileStore(path)
	if err := st.Upsert(&ProjectConfig{Name: "x", RepoPath: "/r"}); err != nil {
		t.Fatalf("Upsert after null JSON: %v", err)
	}
	got, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil || len(got) != 1 || got["x"] == nil {
		t.Errorf("expected {x: <cfg>}, got %+v", got)
	}
}

// TestLoad_NullJSON exercises the `if projects == nil` fallback in
// Load(). The literal JSON `null` makes json.Unmarshal assign a nil map;
// Load must substitute a non-nil empty map.
func TestLoad_NullJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "null.json")
	if err := os.WriteFile(path, []byte(nullJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	st := pointFileStore(path)
	got, err := st.Load()
	if err != nil {
		t.Fatalf("Load on null JSON: %v", err)
	}
	if got == nil {
		t.Fatal("Load must substitute nil map for non-nil empty map")
	}
	if len(got) != 0 {
		t.Errorf("Load on null JSON returned %v, want empty", got)
	}
}
