package web

import (
	"path/filepath"
	"testing"
)

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
