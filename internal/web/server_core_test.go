package web

import (
	"path/filepath"
	"testing"
)

func TestServerAppendAndListCopiesEntries(t *testing.T) {
	srv := &Server{}
	srv.Append(map[string]interface{}{"event": "one"})
	srv.Append(map[string]interface{}{"event": "two"})

	entries := srv.List()
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	entries[0]["event"] = "mutated"

	again := srv.List()
	if again[0]["event"] != "one" {
		t.Fatalf("List returned shared backing storage: got %v", again[0]["event"])
	}
}

func TestNewServerWiresRouterAndRunner(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "projects.json")
	srv := NewServer(Config{
		ListenAddr: ":0",
		StorePath:  storePath,
	})
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
	if srv.router == nil {
		t.Fatal("expected router to be initialized")
	}
	if srv.pipelineRunner == nil {
		t.Fatal("expected pipeline runner to be wired")
	}
	if srv.taskRunner == nil {
		t.Fatal("expected task runner to be wired")
	}
	if srv.runService == nil {
		t.Fatal("expected run service to be wired")
	}
}
