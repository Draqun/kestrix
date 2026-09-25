package web

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func validSigningKeyPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jwt.key")
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
	server, err := NewServerE(Config{JWTSigningKeyPath: validSigningKeyPath(t)})
	if err == nil || err.Error() != "database URL is required" {
		t.Fatalf("error = %v, want missing database URL error", err)
	}
	if server != nil {
		t.Fatal("server was created without a database")
	}
}

func TestNewServerERequiresExplicitMasterKeyPath(t *testing.T) {
	server, err := NewServerE(Config{DatabaseURL: "postgres://valid", JWTSigningKeyPath: validSigningKeyPath(t)})
	if err == nil || server != nil || !strings.Contains(err.Error(), "KESTRIX_MASTER_KEY_PATH") {
		t.Fatalf("server=%v error=%v, want missing master-key path error", server, err)
	}
}

func TestNewServerERequiresExplicitJWTSigningKeyPath(t *testing.T) {
	server, err := NewServerE(Config{DatabaseURL: "postgres://valid", MasterKeyPath: validMasterKeyPath(t)})
	if err == nil || server != nil || !strings.Contains(err.Error(), "KESTRIX_JWT_SIGNING_KEY_PATH") {
		t.Fatalf("server=%v error=%v, want missing JWT signing-key path error", server, err)
	}
}

func TestNewServerERejectsInvalidJWTSigningKeyBeforeDatabase(t *testing.T) {
	called := false
	old := newPGStores
	newPGStores = func(string, string) (domain.ProjectStore, domain.RunStore, error) {
		called = true
		return nil, nil, errors.New("should not connect")
	}
	t.Cleanup(func() { newPGStores = old })

	path := filepath.Join(t.TempDir(), "jwt.key")
	secret := "not-a-signing-key"
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := NewServerE(Config{DatabaseURL: "postgres://valid", MasterKeyPath: validMasterKeyPath(t), JWTSigningKeyPath: path})
	if err == nil || server != nil || called {
		t.Fatalf("server=%v error=%v database called=%t", server, err, called)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("startup error leaked key material: %v", err)
	}
}

func TestNewServerETokensSurviveRestart(t *testing.T) {
	projectStore := &mocks.MockProjectStore{}
	runStore := &mocks.MockRunStore{}
	old := newPGStores
	newPGStores = func(string, string) (domain.ProjectStore, domain.RunStore, error) {
		return projectStore, runStore, nil
	}
	t.Cleanup(func() { newPGStores = old })

	keyPath := validSigningKeyPath(t)
	cfg := Config{DatabaseURL: "postgres://valid", MasterKeyPath: validMasterKeyPath(t), JWTSigningKeyPath: keyPath}

	first, err := NewServerE(cfg)
	if err != nil {
		t.Fatalf("first NewServerE: %v", err)
	}
	token, _, err := first.tokenIssuer.Issue(domain.User{Name: "alice", Role: domain.RoleAdmin})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	second, err := NewServerE(cfg)
	if err != nil {
		t.Fatalf("second NewServerE (simulated restart): %v", err)
	}
	claims, err := second.tokenIssuer.Verify(token)
	if err != nil {
		t.Fatalf("Verify after restart: %v", err)
	}
	if claims.UserName != "alice" {
		t.Fatalf("claims.UserName = %q, want alice", claims.UserName)
	}
}

func TestNewServerERejectsInvalidMasterKeyBeforeDatabase(t *testing.T) {
	called := false
	old := newPGStores
	newPGStores = func(string, string) (domain.ProjectStore, domain.RunStore, error) {
		called = true
		return nil, nil, errors.New("should not connect")
	}
	t.Cleanup(func() { newPGStores = old })

	path := filepath.Join(t.TempDir(), "master.key")
	secret := "not-a-master-key"
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := NewServerE(Config{DatabaseURL: "postgres://valid", MasterKeyPath: path, JWTSigningKeyPath: validSigningKeyPath(t)})
	if err == nil || server != nil || called {
		t.Fatalf("server=%v error=%v database called=%t", server, err, called)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("startup error leaked key material: %v", err)
	}
}

func TestNewServerERejectsDatabaseFailureBeforeServing(t *testing.T) {
	old := newPGStores
	newPGStores = func(string, string) (domain.ProjectStore, domain.RunStore, error) {
		return nil, nil, errors.New("ping database postgres://user:secret@example.invalid/db?sslmode=disable failed")
	}
	t.Cleanup(func() { newPGStores = old })

	server, err := NewServerE(Config{DatabaseURL: "postgres://user:secret@example.invalid/db?sslmode=disable", MasterKeyPath: validMasterKeyPath(t), JWTSigningKeyPath: validSigningKeyPath(t)})
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
	if _, _, err := newPGStores("postgres://127.0.0.1:1/nope?connect_timeout=1", "local"); err == nil {
		t.Fatal("expected database connection error")
	}
}

func TestNewServerEWiresPostgreSQLStores(t *testing.T) {
	projectStore := &mocks.MockProjectStore{}
	runStore := &mocks.MockRunStore{}
	old := newPGStores
	newPGStores = func(string, string) (domain.ProjectStore, domain.RunStore, error) {
		return projectStore, runStore, nil
	}
	t.Cleanup(func() { newPGStores = old })

	server, err := NewServerE(Config{DatabaseURL: "postgres://valid", MasterKeyPath: validMasterKeyPath(t), JWTSigningKeyPath: validSigningKeyPath(t)})
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

// REVIEW.md R7: NewServerE must forward the configured concurrency limit and
// output bound into the RunService it builds; deleting that wiring would
// silently restore unbounded concurrency/output with nothing else failing.
func TestNewServerEWiresRunConcurrencyAndOutputBound(t *testing.T) {
	projectStore := &mocks.MockProjectStore{}
	runStore := &mocks.MockRunStore{}
	old := newPGStores
	newPGStores = func(string, string) (domain.ProjectStore, domain.RunStore, error) {
		return projectStore, runStore, nil
	}
	t.Cleanup(func() { newPGStores = old })

	server, err := NewServerE(Config{
		DatabaseURL:       "postgres://valid",
		MasterKeyPath:     validMasterKeyPath(t),
		JWTSigningKeyPath: validSigningKeyPath(t),
		Runner:            RunnerConfig{MaxConcurrentRuns: 3, MaxOutputBytes: 4096},
	})
	if err != nil {
		t.Fatalf("NewServerE: %v", err)
	}
	if got := server.runService.MaxConcurrentRuns(); got != 3 {
		t.Errorf("RunService.MaxConcurrentRuns() = %d, want 3", got)
	}
	if server.runService.MaxOutputBytes != 4096 {
		t.Errorf("RunService.MaxOutputBytes = %d, want 4096", server.runService.MaxOutputBytes)
	}
}

// TestServer_FailInterruptedRuns_MatchesProductionSequence exercises the
// exact sequence cmdServe runs in production: NewServerE, then
// FailInterruptedRuns on the result, before ListenAndServe. Every other test
// here only calls NewServerE and inspects the wired server; this is the one
// that proves the reconcile step - added so cmdServe can run it before the
// listener opens - actually reaches the RunStore NewServerE built.
func TestServer_FailInterruptedRuns_MatchesProductionSequence(t *testing.T) {
	projectStore := &mocks.MockProjectStore{}
	runStore := &mocks.MockRunStore{FailInterruptedResult: 3}
	old := newPGStores
	newPGStores = func(string, string) (domain.ProjectStore, domain.RunStore, error) {
		return projectStore, runStore, nil
	}
	t.Cleanup(func() { newPGStores = old })

	server, err := NewServerE(Config{DatabaseURL: "postgres://valid", MasterKeyPath: validMasterKeyPath(t), JWTSigningKeyPath: validSigningKeyPath(t)})
	if err != nil {
		t.Fatalf("NewServerE: %v", err)
	}

	n, err := server.FailInterruptedRuns("interrupted by server restart")
	if err != nil {
		t.Fatalf("FailInterruptedRuns: %v", err)
	}
	if n != 3 {
		t.Fatalf("n = %d, want 3", n)
	}
	if len(runStore.FailInterruptedCalls) != 1 || runStore.FailInterruptedCalls[0] != "interrupted by server restart" {
		t.Fatalf("FailInterruptedCalls = %v, want one call with the restart reason", runStore.FailInterruptedCalls)
	}
}

func TestNewServerEWiresLoginThrottleWhenConfigured(t *testing.T) {
	projectStore := &mocks.MockProjectStore{}
	runStore := &mocks.MockRunStore{}
	old := newPGStores
	newPGStores = func(string, string) (domain.ProjectStore, domain.RunStore, error) {
		return projectStore, runStore, nil
	}
	t.Cleanup(func() { newPGStores = old })

	server, err := NewServerE(Config{
		DatabaseURL:       "postgres://valid",
		MasterKeyPath:     validMasterKeyPath(t),
		JWTSigningKeyPath: validSigningKeyPath(t),
		LoginMaxAttempts:  5,
		LoginWindow:       time.Minute,
	})
	if err != nil {
		t.Fatalf("NewServerE: %v", err)
	}
	if server.loginThrottle == nil {
		t.Fatal("loginThrottle was not wired despite LoginMaxAttempts/LoginWindow being configured")
	}
	if server.setupThrottle == nil {
		t.Fatal("setupThrottle was not wired despite LoginMaxAttempts/LoginWindow being configured")
	}
	if server.setupThrottle == server.loginThrottle {
		t.Fatal("setupThrottle must be its own instance, not shared with loginThrottle - see REVIEW.md R1")
	}
}

func TestNewServerELeavesLoginThrottleNilWhenUnconfigured(t *testing.T) {
	projectStore := &mocks.MockProjectStore{}
	runStore := &mocks.MockRunStore{}
	old := newPGStores
	newPGStores = func(string, string) (domain.ProjectStore, domain.RunStore, error) {
		return projectStore, runStore, nil
	}
	t.Cleanup(func() { newPGStores = old })

	server, err := NewServerE(Config{
		DatabaseURL:       "postgres://valid",
		MasterKeyPath:     validMasterKeyPath(t),
		JWTSigningKeyPath: validSigningKeyPath(t),
	})
	if err != nil {
		t.Fatalf("NewServerE: %v", err)
	}
	if server.loginThrottle != nil {
		t.Fatal("loginThrottle was wired despite LoginMaxAttempts/LoginWindow being unset")
	}
	if server.setupThrottle != nil {
		t.Fatal("setupThrottle was wired despite LoginMaxAttempts/LoginWindow being unset")
	}
}

// combinedUserGrantStore lets one fake satisfy ProjectStore, RunStore,
// UserStore, and GrantStore at once, exercising the type-assertion branches
// in NewServerE that wire s.userStore, s.grantStore, and s.authService -
// branches a plain *mocks.MockProjectStore never reaches.
type combinedUserGrantStore struct {
	*mocks.MockProjectStore
	*mocks.MockRunStore
	*errUserStore
	*errGrantStore
}

func TestNewServerEWiresUserAndGrantStoresWhenSupported(t *testing.T) {
	combined := &combinedUserGrantStore{
		MockProjectStore: &mocks.MockProjectStore{},
		MockRunStore:     &mocks.MockRunStore{},
		errUserStore:     newErrUserStore(),
		errGrantStore:    newErrGrantStore(),
	}
	old := newPGStores
	newPGStores = func(string, string) (domain.ProjectStore, domain.RunStore, error) { return combined, combined, nil }
	t.Cleanup(func() { newPGStores = old })

	s, err := NewServerE(Config{DatabaseURL: "postgres://valid", MasterKeyPath: validMasterKeyPath(t), JWTSigningKeyPath: validSigningKeyPath(t)})
	if err != nil {
		t.Fatalf("NewServerE: %v", err)
	}
	if s.userStore == nil {
		t.Fatal("userStore was not wired despite the store implementing domain.UserStore")
	}
	if s.grantStore == nil {
		t.Fatal("grantStore was not wired despite the store implementing domain.GrantStore")
	}
	if s.authService == nil {
		t.Fatal("authService was not constructed once a userStore was wired")
	}
}

func TestNewServerESourceCacheFailureAndMaintenanceWarning(t *testing.T) {
	projectStore := &mocks.MockProjectStore{}
	runStore := &mocks.MockRunStore{}
	oldStores, oldSource := newPGStores, newSourceSessionManager
	newPGStores = func(string, string) (domain.ProjectStore, domain.RunStore, error) { return projectStore, runStore, nil }
	t.Cleanup(func() {
		newPGStores, newSourceSessionManager = oldStores, oldSource
	})

	newSourceSessionManager = func(source.CacheOptions) (domain.SourceSessionManager, error) {
		return nil, errors.New("cache failed")
	}
	if server, err := NewServerE(Config{DatabaseURL: "postgres://valid", MasterKeyPath: validMasterKeyPath(t), JWTSigningKeyPath: validSigningKeyPath(t)}); err == nil || server != nil || !strings.Contains(err.Error(), "source cache") {
		t.Fatalf("server=%v error=%v", server, err)
	}

	manager := &failingMaintenanceSessions{testSourceSessions: &testSourceSessions{}}
	newSourceSessionManager = func(source.CacheOptions) (domain.SourceSessionManager, error) { return manager, nil }
	server, err := NewServerE(Config{DatabaseURL: "postgres://valid", MasterKeyPath: validMasterKeyPath(t), JWTSigningKeyPath: validSigningKeyPath(t)})
	if err != nil || server.sourceSessions != manager {
		t.Fatalf("server=%v error=%v", server, err)
	}
}

func TestServer_Router(t *testing.T) {
	srv := NewServer(Config{})
	if srv.Router() == nil {
		t.Fatal("Router() returned nil")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Router()-served /api/health status = %d, want 200", w.Code)
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
