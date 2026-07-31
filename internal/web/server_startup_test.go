package web

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
)

func TestNewServerERequiresDatabaseURL(t *testing.T) {
	server, err := NewServerE(Config{})
	if err == nil || err.Error() != "database URL is required" {
		t.Fatalf("error = %v, want missing database URL error", err)
	}
	if server != nil {
		t.Fatal("server was created without a database")
	}
}

func TestNewServerERejectsDatabaseFailureBeforeServing(t *testing.T) {
	old := newPGStores
	newPGStores = func(string) (domain.ProjectStore, domain.RunStore, error) {
		return nil, nil, errors.New("ping database postgres://user:secret@example.invalid/db?sslmode=disable failed")
	}
	t.Cleanup(func() { newPGStores = old })

	server, err := NewServerE(Config{DatabaseURL: "postgres://user:secret@example.invalid/db?sslmode=disable"})
	if err == nil || server != nil {
		t.Fatalf("server=%v error=%v, want startup failure", server, err)
	}
	if err.Error() == "" {
		t.Fatal("startup error is empty")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("startup error leaked database credentials: %v", err)
	}
}

func TestNewServerEWiresPostgreSQLStores(t *testing.T) {
	projectStore := &mocks.MockProjectStore{}
	runStore := &mocks.MockRunStore{}
	old := newPGStores
	newPGStores = func(string) (domain.ProjectStore, domain.RunStore, error) {
		return projectStore, runStore, nil
	}
	t.Cleanup(func() { newPGStores = old })

	server, err := NewServerE(Config{DatabaseURL: "postgres://valid"})
	if err != nil {
		t.Fatalf("NewServerE: %v", err)
	}
	if server.projectStore != projectStore || server.runStore != runStore {
		t.Fatal("server did not use PostgreSQL stores")
	}
	if server.router == nil {
		t.Fatal("server router is nil")
	}
}

func TestNewServerDoesNotAutoRegisterPipelineDirectory(t *testing.T) {
	pipelineDir := t.TempDir()
	pipeline := []byte(`project "demo-mono" {
  ci_repo "app" { path = "app" }
}`)
	if err := os.WriteFile(filepath.Join(pipelineDir, "pipeline.tf"), pipeline, 0o644); err != nil {
		t.Fatalf("write pipeline: %v", err)
	}

	server := NewServer(Config{PipelineDir: pipelineDir})
	projects, err := server.projectStore.List()
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("startup auto-registered projects: %v", projects)
	}
}
