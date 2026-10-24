package web

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kestrix/internal/credentials"
	"kestrix/internal/domain"
	"kestrix/internal/store"
)

func testIntegrationDBURL(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test")
	}
	dbURL := os.Getenv("KESTRIX_TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://kestrix:kestrix@localhost:5433/kestrix_test?sslmode=disable&connect_timeout=2"
	}
	parsed, err := url.Parse(dbURL)
	if err == nil {
		dbName := strings.TrimPrefix(parsed.Path, "/")
		if !strings.HasSuffix(dbName, "_test") {
			t.Fatalf("tests must use a database ending with _test (got %q); set KESTRIX_TEST_DATABASE_URL", dbName)
		}
	}
	return dbURL
}

func writeKeyFile(t *testing.T, dir, name string, b byte) string {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = b
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, key, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNewServerE_MasterKeyIntegration_MismatchAndLock(t *testing.T) {
	dbURL := testIntegrationDBURL(t)

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		t.Skipf("PostgreSQL ping failed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM credentials`)
		db.Close()
	})
	_, _ = db.Exec(`DELETE FROM credentials`)

	tmpDir := t.TempDir()
	keyAPath := writeKeyFile(t, tmpDir, "keyA.key", 0x11)
	keyBPath := writeKeyFile(t, tmpDir, "keyB.key", 0x22)
	signingPath := validSigningKeyPath(t)

	// Scenario 1: Empty credentials - starts fine with key A
	srvA, err := NewServerE(Config{
		DatabaseURL:       dbURL,
		MasterKeyPath:     keyAPath,
		JWTSigningKeyPath: signingPath,
		Runner:            RunnerConfig{AllowLocal: true, Mode: "local"},
	})
	if err != nil {
		t.Fatalf("NewServerE with empty credentials: %v", err)
	}

	// Verify advisory lock is held
	var locked bool
	if err := db.QueryRow("SELECT pg_try_advisory_lock($1)", store.ServerAdvisoryLockKey).Scan(&locked); err != nil {
		t.Fatalf("try advisory lock: %v", err)
	}
	if locked {
		t.Fatalf("expected server advisory lock to be held by srvA")
	}

	// Close srvA
	if err := srvA.Close(); err != nil {
		t.Fatalf("srvA.Close: %v", err)
	}

	// Verify advisory lock was released
	if err := db.QueryRow("SELECT pg_try_advisory_lock($1)", store.ServerAdvisoryLockKey).Scan(&locked); err != nil {
		t.Fatalf("try advisory lock after close: %v", err)
	}
	if !locked {
		t.Fatalf("expected advisory lock to be free after close")
	}
	_, _ = db.Exec("SELECT pg_advisory_unlock($1)", store.ServerAdvisoryLockKey)

	// Insert a credential encrypted with key A
	pgStore, err := store.NewPG(dbURL, "local")
	if err != nil {
		t.Fatalf("NewPG: %v", err)
	}
	defer pgStore.Close()
	pgStore.SetCredentialMasterKeyProvider(credentials.FileMasterKeyProvider{Path: keyAPath, Version: 1})
	_, err = pgStore.CreateCredential(domain.CredentialInput{
		Name:     "integration-cred",
		Type:     "token",
		Provider: "gitlab",
		Host:     "gitlab.com",
		Token:    "glpat-token-secret",
	})
	if err != nil {
		t.Fatalf("CreateCredential with key A: %v", err)
	}

	// Scenario 2: Startup with key A (matching) succeeds
	srvA2, err := NewServerE(Config{
		DatabaseURL:       dbURL,
		MasterKeyPath:     keyAPath,
		JWTSigningKeyPath: signingPath,
		Runner:            RunnerConfig{AllowLocal: true, Mode: "local"},
	})
	if err != nil {
		t.Fatalf("NewServerE with matching key A: %v", err)
	}
	if err := srvA2.Close(); err != nil {
		t.Fatalf("srvA2.Close: %v", err)
	}

	// Scenario 3: Startup with key B (mismatched) fails before serving requests
	srvB, err := NewServerE(Config{
		DatabaseURL:       dbURL,
		MasterKeyPath:     keyBPath,
		JWTSigningKeyPath: signingPath,
		Runner:            RunnerConfig{AllowLocal: true, Mode: "local"},
	})
	if err == nil || srvB != nil {
		t.Fatalf("expected startup failure with mismatched key, got server=%v err=%v", srvB, err)
	}
	wantMsg := "the configured master key does not decrypt stored credentials; restore the previous key or complete a rotation with 'kestrix keys rotate-master'"
	if !strings.Contains(err.Error(), wantMsg) {
		t.Fatalf("error %q does not contain expected %q", err.Error(), wantMsg)
	}
}
