package web

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/source"
)

type failingMaintenanceSessions struct{ *testSourceSessions }

func (f *failingMaintenanceSessions) Maintain(context.Context) error {
	return errors.New("maintenance failed")
}

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

func TestNewPGStoresRedactsUnavailableDatabase(t *testing.T) {
	if _, _, err := newPGStores("postgres://127.0.0.1:1/nope?connect_timeout=1"); err == nil {
		t.Fatal("expected database connection error")
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
	if server.webhookService == nil {
		t.Fatal("webhook service was not wired")
	}
}

func TestNewServerESourceCacheFailureAndMaintenanceWarning(t *testing.T) {
	projectStore := &mocks.MockProjectStore{}
	runStore := &mocks.MockRunStore{}
	oldStores, oldSource := newPGStores, newSourceSessionManager
	newPGStores = func(string) (domain.ProjectStore, domain.RunStore, error) { return projectStore, runStore, nil }
	t.Cleanup(func() {
		newPGStores, newSourceSessionManager = oldStores, oldSource
	})

	newSourceSessionManager = func(source.CacheOptions) (domain.SourceSessionManager, error) {
		return nil, errors.New("cache failed")
	}
	if server, err := NewServerE(Config{DatabaseURL: "postgres://valid"}); err == nil || server != nil || !strings.Contains(err.Error(), "source cache") {
		t.Fatalf("server=%v error=%v", server, err)
	}

	manager := &failingMaintenanceSessions{testSourceSessions: &testSourceSessions{}}
	newSourceSessionManager = func(source.CacheOptions) (domain.SourceSessionManager, error) { return manager, nil }
	server, err := NewServerE(Config{DatabaseURL: "postgres://valid"})
	if err != nil || server.sourceSessions != manager {
		t.Fatalf("server=%v error=%v", server, err)
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
