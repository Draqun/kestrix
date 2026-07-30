package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarshalErrors(t *testing.T) {
	old := marshalJSON
	marshalJSON = func(any, string, string) ([]byte, error) { return nil, os.ErrInvalid }
	t.Cleanup(func() { marshalJSON = old })
	st := pointFileStore(filepath.Join(t.TempDir(), "projects.json"))
	if err := st.Save(map[string]*ProjectConfig{}); err == nil || !strings.Contains(err.Error(), "marshal projects") {
		t.Fatalf("Save error = %v, want marshal projects", err)
	}
	if err := st.Upsert(&ProjectConfig{Name: "x"}); err == nil || !strings.Contains(err.Error(), "marshal projects") {
		t.Fatalf("Upsert error = %v, want marshal projects", err)
	}
}

func TestSaveLockedRenameError(t *testing.T) {
	old := renameFile
	renameFile = func(string, string) error { return os.ErrInvalid }
	t.Cleanup(func() { renameFile = old })
	st := pointFileStore(filepath.Join(t.TempDir(), "projects.json"))
	if err := st.Upsert(&ProjectConfig{Name: "x"}); err == nil || !strings.Contains(err.Error(), "rename temp") {
		t.Fatalf("Upsert error = %v, want rename temp", err)
	}
}

func TestMarshalJSONSignature(t *testing.T) {
	if _, err := json.MarshalIndent(map[string]string{"ok": "ok"}, "", "  "); err != nil {
		t.Fatal(err)
	}
}

func TestNewParentMkdirError(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "file")
	if err := os.WriteFile(parent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(filepath.Join(parent, "projects.json")); err == nil || !strings.Contains(err.Error(), "create store dir") {
		t.Fatalf("New error = %v, want create store dir", err)
	}
}

func TestSaveWriteTempError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "projects.json")
	if err := os.Mkdir(path+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	st := pointFileStore(path)
	if err := st.Save(map[string]*ProjectConfig{}); err == nil || !strings.Contains(err.Error(), "write temp") {
		t.Fatalf("Save error = %v, want write temp", err)
	}
}

func TestSaveRenameError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "projects.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	st := pointFileStore(path)
	if err := st.Save(map[string]*ProjectConfig{}); err == nil || !strings.Contains(err.Error(), "rename temp") {
		t.Fatalf("Save error = %v, want rename temp", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file should be removed, stat err=%v", err)
	}
}

func TestLoadAndListReadErrors(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "directory")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	st := pointFileStore(path)
	if _, err := st.Load(); err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("Load error = %v, want read", err)
	}
	if _, err := st.List(); err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("List error = %v, want read", err)
	}
	if _, err := st.FindByGitLabID(1); err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("FindByGitLabID error = %v, want read", err)
	}
	if err := st.Upsert(&ProjectConfig{Name: "x"}); err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("Upsert error = %v, want read", err)
	}
}

func TestUpsertDefaultsMigratePipelinePath(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ProjectConfig{Name: "migrate", PipelineFile: "ci/pipeline.hcl"}
	if err := st.Upsert(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.PipelineDir != "ci" || cfg.PipelineFile != "pipeline.hcl" || cfg.DefaultBranch != "main" || cfg.CreatedAt == "" {
		t.Fatalf("defaults = %+v", cfg)
	}
}
