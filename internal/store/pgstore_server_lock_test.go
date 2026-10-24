package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"kestrix/internal/credentials"
	"kestrix/internal/domain"
)

func TestPG_ServerAdvisoryLock_HeldAndReleased(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()

	// Initially, we should be able to try-lock with another connection.
	conn, err := pg.db.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn: %v", err)
	}
	defer conn.Close()

	// Server holds lock
	if err := pg.HoldServerLock(ctx); err != nil {
		t.Fatalf("HoldServerLock: %v", err)
	}
	// Calling HoldServerLock again should be idempotent
	if err := pg.HoldServerLock(ctx); err != nil {
		t.Fatalf("HoldServerLock second call: %v", err)
	}

	// Another connection tries to acquire the lock: should fail (locked == false)
	var locked bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", ServerAdvisoryLockKey).Scan(&locked); err != nil {
		t.Fatalf("try advisory lock query: %v", err)
	}
	if locked {
		t.Fatalf("expected advisory lock to be held, but got locked=true")
	}

	// TryServerLock should also return false
	gotLocked, closer, err := pg.TryServerLock(ctx)
	if err != nil {
		t.Fatalf("TryServerLock: %v", err)
	}
	if gotLocked || closer != nil {
		t.Fatalf("expected TryServerLock to fail while server holds lock")
	}

	// Release the server lock
	if err := pg.ReleaseServerLock(); err != nil {
		t.Fatalf("ReleaseServerLock: %v", err)
	}

	// Now another connection should be able to acquire the lock
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", ServerAdvisoryLockKey).Scan(&locked); err != nil {
		t.Fatalf("try advisory lock query after release: %v", err)
	}
	if !locked {
		t.Fatalf("expected advisory lock to be free, but got locked=false")
	}
	// Unlock so we leave it clean
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", ServerAdvisoryLockKey); err != nil {
		t.Fatalf("unlock: %v", err)
	}

	// TryServerLock should now succeed
	gotLocked, closer, err = pg.TryServerLock(ctx)
	if err != nil {
		t.Fatalf("TryServerLock: %v", err)
	}
	if !gotLocked || closer == nil {
		t.Fatalf("expected TryServerLock to succeed")
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("closer.Close: %v", err)
	}
	// Idempotent close
	if err := closer.Close(); err != nil {
		t.Fatalf("second closer.Close: %v", err)
	}
}

func TestPG_CheckMasterKey(t *testing.T) {
	pg := testPG(t)
	_, _ = pg.db.Exec(`DELETE FROM credentials`)

	key1 := make([]byte, 32)
	for i := range key1 {
		key1[i] = byte(i + 1)
	}
	prov1 := credentials.StaticMasterKeyProvider{
		Version: 1,
		Keys:    map[int][]byte{1: key1},
	}

	// 1. Empty credentials: check passes
	if err := pg.CheckMasterKey(prov1); err != nil {
		t.Fatalf("CheckMasterKey with empty credentials: %v", err)
	}

	// Nil provider fails
	if err := pg.CheckMasterKey(nil); !errors.Is(err, credentials.ErrMasterKeyUnavailable) {
		t.Fatalf("CheckMasterKey(nil) = %v, want ErrMasterKeyUnavailable", err)
	}

	// 2. Insert a credential using prov1
	pg.SetCredentialMasterKeyProvider(prov1)
	meta, err := pg.CreateCredential(domain.CredentialInput{
		Name:     "repo-cred",
		Type:     "token",
		Provider: "gitlab",
		Host:     "gitlab.com",
		Token:    "glpat-secret123",
	})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	t.Cleanup(func() {
		_ = pg.RevokeCredential(meta.Ref)
		_, _ = pg.db.Exec(`DELETE FROM credentials WHERE credential_ref = $1`, meta.Ref)
	})

	// 3. Check with prov1: matches, check passes
	if err := pg.CheckMasterKey(prov1); err != nil {
		t.Fatalf("CheckMasterKey matching key: %v", err)
	}

	// 4. Check with prov2 (different key): mismatch, returns ErrMasterKeyMismatch
	key2 := make([]byte, 32)
	for i := range key2 {
		key2[i] = byte(i + 99)
	}
	prov2 := credentials.StaticMasterKeyProvider{
		Version: 1,
		Keys:    map[int][]byte{1: key2},
	}

	err = pg.CheckMasterKey(prov2)
	if err == nil {
		t.Fatalf("CheckMasterKey mismatching key: expected error, got nil")
	}
	if !errors.Is(err, ErrMasterKeyMismatch) {
		t.Fatalf("CheckMasterKey err = %v, want ErrMasterKeyMismatch", err)
	}
	wantMsg := "the configured master key does not decrypt stored credentials; restore the previous key or complete a rotation with 'kestrix keys rotate-master'"
	if !strings.Contains(err.Error(), wantMsg) {
		t.Fatalf("CheckMasterKey err msg = %q, want it to contain %q", err.Error(), wantMsg)
	}
}

func TestPG_Close_ReleasesServerLock(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	dbURL := os.Getenv("KESTRIX_TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://kestrix:kestrix@localhost:5433/kestrix_test?sslmode=disable&connect_timeout=2"
	}
	pg, err := NewPG(dbURL, "local")
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	ctx := context.Background()

	if err := pg.HoldServerLock(ctx); err != nil {
		t.Fatalf("HoldServerLock: %v", err)
	}

	// Close PGStore
	if err := pg.Close(); err != nil {
		t.Fatalf("pg.Close: %v", err)
	}
	// Calling Close again is safe
	if err := pg.Close(); err != nil {
		t.Fatalf("pg.Close second: %v", err)
	}

	newDB, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open new db: %v", err)
	}
	defer newDB.Close()

	var locked bool
	if err := newDB.QueryRow("SELECT pg_try_advisory_lock($1)", ServerAdvisoryLockKey).Scan(&locked); err != nil {
		t.Fatalf("try advisory lock on new db: %v", err)
	}
	if !locked {
		t.Fatalf("expected advisory lock to be free after PGStore.Close()")
	}
	_, _ = newDB.Exec("SELECT pg_advisory_unlock($1)", ServerAdvisoryLockKey)
}
