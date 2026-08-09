package web

import (
	"context"
	"crypto/rand"
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

func validMasterKeyPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "master.key")
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

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

func TestNewServerERequiresExplicitMasterKeyPath(t *testing.T) {
	server, err := NewServerE(Config{DatabaseURL: "postgres://valid"})
	if err == nil || server != nil || !strings.Contains(err.Error(), "KESTRIX_MASTER_KEY_PATH") {
		t.Fatalf("server=%v error=%v, want missing master-key path error", server, err)
	}
}

func TestNewServerERejectsInvalidMasterKeyBeforeDatabase(t *testing.T) {
	called := false
	old := newPGStores
	newPGStores = func(string) (domain.ProjectStore, domain.RunStore, error) {
		called = true
		return nil, nil, errors.New("should not connect")
	}
	t.Cleanup(func() { newPGStores = old })

	path := filepath.Join(t.TempDir(), "master.key")
	secret := "not-a-master-key"
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := NewServerE(Config{DatabaseURL: "postgres://valid", MasterKeyPath: path})
	if err == nil || server != nil || called {
		t.Fatalf("server=%v error=%v database called=%t", server, err, called)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("startup error leaked key material: %v", err)
	}
}

func TestNewServerERejectsDatabaseFailureBeforeServing(t *testing.T) {
	old := newPGStores
	newPGStores = func(string) (domain.ProjectStore, domain.RunStore, error) {
		return nil, nil, errors.New("ping database postgres://user:secret@example.invalid/db?sslmode=disable failed")
	}
	t.Cleanup(func() { newPGStores = old })

	server, err := NewServerE(Config{DatabaseURL: "postgres://user:secret@example.invalid/db?sslmode=disable", MasterKeyPath: validMasterKeyPath(t)})
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

	server, err := NewServerE(Config{DatabaseURL: "postgres://valid", MasterKeyPath: validMasterKeyPath(t)})
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
	if server, err := NewServerE(Config{DatabaseURL: "postgres://valid", MasterKeyPath: validMasterKeyPath(t)}); err == nil || server != nil || !strings.Contains(err.Error(), "source cache") {
		t.Fatalf("server=%v error=%v", server, err)
	}

	manager := &failingMaintenanceSessions{testSourceSessions: &testSourceSessions{}}
	newSourceSessionManager = func(source.CacheOptions) (domain.SourceSessionManager, error) { return manager, nil }
	server, err := NewServerE(Config{DatabaseURL: "postgres://valid", MasterKeyPath: validMasterKeyPath(t)})
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
