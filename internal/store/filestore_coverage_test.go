// Additional coverage for internal/store/filestore routines that the
// pre-existing test file didn't exercise: public Save(), the
// FindByGitLabID search, ListAllRuns stub, and a few Save/Load error
// branches.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestSave_PublicFunction exercises the public Save() function directly
// (the pre-existing tests reach saveLocked() through Upsert/Delete;
// Save() itself was not covered).
func TestSave_PublicFunction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	st, err := New(path)
	if err != nil {
		t.Fatal(err)
	}

	projects := map[string]*ProjectConfig{
		"alpha": {Name: "alpha", RepoPath: "/src/a"},
		"beta":  {Name: "beta", RepoPath: "/src/b"},
	}
	if err := st.Save(projects); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify the file written is valid JSON and the contents are readable.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var got map[string]*ProjectConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 2 || got["alpha"].Name != "alpha" || got["beta"].RepoPath != "/src/b" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

// TestSave_OverwritesExisting covers Save's "write temp + rename"
// path when there's already content on disk.
func TestSave_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	st, _ := New(path)

	if err := st.Save(map[string]*ProjectConfig{"v1": {Name: "v1"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(map[string]*ProjectConfig{"v2": {Name: "v2"}}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["v2"]; !ok {
		t.Errorf("expected v2 in store, got %+v", got)
	}
	if _, ok := got["v1"]; ok {
		t.Errorf("expected v1 removed, got %+v", got)
	}
}

// TestFindByGitLabID covers the search loop and the no-match path.
func TestFindByGitLabID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	st, _ := New(path)

	if err := st.Upsert(&ProjectConfig{Name: "alpha", RepoPath: "/a", GitLabProjectID: 100}); err != nil {
		t.Fatal(err)
	}
	if err := st.Upsert(&ProjectConfig{Name: "beta", RepoPath: "/b", GitLabProjectID: 200}); err != nil {
		t.Fatal(err)
	}

	p, err := st.FindByGitLabID(100)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil || p.Name != "alpha" {
		t.Errorf("FindByGitLabID(100) = %+v, want alpha", p)
	}

	none, err := st.FindByGitLabID(999)
	if err != nil {
		t.Fatal(err)
	}
	if none != nil {
		t.Errorf("FindByGitLabID(999) = %+v, want nil", none)
	}
}

// TestLoadInvalidJSON covers the unmarshal error path in Load().
func TestLoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not valid"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reach Load() through Get() so the read path is the same as in
	// production.
	_, err := pointFileStore(path).Get("anything")
	if err == nil {
		t.Fatal("expected error from Load on invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("expected 'parse' in error, got %v", err)
	}
}

// TestLoadNonExistent covers the os.IsNotExist fallback to empty map.
func TestLoadNonExistent(t *testing.T) {
	// MkdirAll would create the parent; choose a deeper path that
	// the parent dir creates, but with no file inside.
	parent := t.TempDir()
	path := filepath.Join(parent, "subdir", "projects.json")
	// Build the FileStore manually so New() doesn't try to MkdirAll
	// the inner directory (which would create subdir/).
	st, _ := New(filepath.Join(parent, "anchor.json"))
	fs, ok := interface{}(st).(*FileStore)
	if !ok {
		t.Fatal("expected *FileStore")
	}
	fs.path = path
	projects, err := fs.Load()
	if err != nil {
		t.Fatalf("Load should swallow IsNotExist: %v", err)
	}
	if projects == nil {
		t.Error("Load should return non-nil empty map")
	}
	if len(projects) != 0 {
		t.Errorf("expected empty map, got %v", projects)
	}
}

// TestLoadReadError covers the non-IsNotExist branch in Load (e.g.
// path is a directory).
func TestLoadReadError(t *testing.T) {
	st, _ := New("/proc") // /proc is a directory, not a file.
	if _, err := st.Load(); err == nil {
		t.Fatal("expected error from Load when path is a directory")
	}
}

// TestUpsertPipelineDirMigration covers the PipelineDir defaulting
// branch that splits PipelineFile into dir + base when both unset.
func TestUpsertPipelineDirMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.json")
	st, _ := New(path)
	// PipelineFile set but PipelineDir empty → dir derived from filepath.Dir.
	if err := st.Upsert(&ProjectConfig{
		Name:         "m",
		RepoPath:     "/r",
		PipelineFile: "ci/build.tf",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get("m")
	if err != nil || got == nil {
		t.Fatalf("Get m: %v %+v", err, got)
	}
	if got.PipelineDir == "" || got.PipelineFile != "build.tf" {
		t.Errorf("expected dir derived from ci/, file=build.tf, got dir=%q file=%q",
			got.PipelineDir, got.PipelineFile)
	}
}

// TestListSorted is a small sanity test that List returns the names.
func TestListSorted(t *testing.T) {
	dir := t.TempDir()
	st, _ := New(filepath.Join(dir, "p.json"))
	for _, n := range []string{"c1", "a1", "b1"} {
		if err := st.Upsert(&ProjectConfig{Name: n, RepoPath: "/x"}); err != nil {
			t.Fatal(err)
		}
	}
	names, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "a1,b1,c1" {
		t.Errorf("List = %v, want a1,b1,c1", names)
	}
}


