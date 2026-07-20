package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".metaci", "projects.json")

	st, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if st == nil {
		t.Fatal("New returned nil store")
	}
	// Directory should be created.
	if _, err := os.Stat(filepath.Dir(path)); os.IsNotExist(err) {
		t.Fatal("parent directory not created")
	}
}

func TestLoadEmpty(t *testing.T) {
	dir := t.TempDir()
	st, _ := New(filepath.Join(dir, "projects.json"))

	projects, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("expected empty map, got %d projects", len(projects))
	}
}

func TestUpsertAndGet(t *testing.T) {
	dir := t.TempDir()
	st, _ := New(filepath.Join(dir, "projects.json"))

	cfg := &ProjectConfig{
		Name:         "my-project",
		RepoPath:     "/src/my-project",
		PipelineFile: "pipeline.tf",
	}

	if err := st.Upsert(cfg); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := st.Get("my-project")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.RepoPath != "/src/my-project" {
		t.Errorf("RepoPath = %q, want %q", got.RepoPath, "/src/my-project")
	}
	if got.CreatedAt == "" {
		t.Error("CreatedAt not set")
	}
	if got.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q, want %q", got.DefaultBranch, "main")
	}
	if got.PipelineFile != "pipeline.tf" {
		t.Errorf("PipelineFile = %q, want %q", got.PipelineFile, "pipeline.tf")
	}
}

func TestUpsertOverwrite(t *testing.T) {
	dir := t.TempDir()
	st, _ := New(filepath.Join(dir, "projects.json"))

	st.Upsert(&ProjectConfig{Name: "p1", RepoPath: "/old"})
	st.Upsert(&ProjectConfig{Name: "p1", RepoPath: "/new"})

	got, _ := st.Get("p1")
	if got.RepoPath != "/new" {
		t.Errorf("RepoPath = %q, want %q", got.RepoPath, "/new")
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	st, _ := New(filepath.Join(dir, "projects.json"))

	st.Upsert(&ProjectConfig{Name: "p1", RepoPath: "/x"})
	if err := st.Delete("p1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, _ := st.Get("p1")
	if got != nil {
		t.Error("project still exists after delete")
	}
}

func TestDeleteNotFound(t *testing.T) {
	dir := t.TempDir()
	st, _ := New(filepath.Join(dir, "projects.json"))

	if err := st.Delete("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent project")
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	st, _ := New(filepath.Join(dir, "projects.json"))

	st.Upsert(&ProjectConfig{Name: "a", RepoPath: "/a"})
	st.Upsert(&ProjectConfig{Name: "b", RepoPath: "/b"})

	names, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
}

func TestPersistAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")

	st1, _ := New(path)
	st1.Upsert(&ProjectConfig{Name: "p1", RepoPath: "/src", GitLabProjectID: 12345})

	// New store from same file.
	st2, _ := New(path)
	got, err := st2.Get("p1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("project not persisted")
	}
	if got.GitLabProjectID != 12345 {
		t.Errorf("GitLabProjectID = %d, want 12345", got.GitLabProjectID)
	}
}

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")

	st, _ := New(path)
	st.Upsert(&ProjectConfig{Name: "p1", RepoPath: "/x"})

	// Temp file should not exist after successful write.
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("temp file still exists after write")
	}

	// Main file should be valid JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(data) == 0 {
		t.Error("file is empty")
	}
}

func TestConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	st, _ := New(filepath.Join(dir, "projects.json"))

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := filepath.Base(t.TempDir()) // unique name
			st.Upsert(&ProjectConfig{Name: name, RepoPath: "/" + name})
		}(i)
	}
	wg.Wait()

	names, _ := st.List()
	if len(names) != 10 {
		t.Errorf("expected 10 projects, got %d", len(names))
	}
}

func TestDefaultPath(t *testing.T) {
	p := DefaultPath()
	if p == "" {
		t.Error("DefaultPath returned empty string")
	}
	if filepath.Base(p) != "projects.json" {
		t.Errorf("DefaultPath base = %q, want %q", filepath.Base(p), "projects.json")
	}
}
