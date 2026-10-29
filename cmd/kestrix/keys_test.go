package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kestrix/internal/config"
	"kestrix/internal/credentials"
	"kestrix/internal/domain"
	"kestrix/internal/store"
)

func writeTestKey(t *testing.T, dir, filename string, content []byte, perm os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, filename)
	if err := os.WriteFile(p, content, perm); err != nil {
		t.Fatal(err)
	}
	return p
}

func withMasterKeyPath(t *testing.T, path string) {
	t.Helper()
	orig := config.Current()
	derived := *orig
	derived.MasterKeyPath = path
	config.ReplaceCurrent(&derived)
	t.Cleanup(func() {
		config.ReplaceCurrent(orig)
	})
}

func TestKeys_UsageAndUnknown(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runKeys(nil, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("runKeys(nil) = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage: kestrix keys") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}

	stderr.Reset()
	code = runKeys([]string{"unknown"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("runKeys(unknown) = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown keys subcommand: unknown") {
		t.Fatalf("stderr = %q, want unknown keys subcommand", stderr.String())
	}
}

func TestKeysRotateMaster_FlagErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer

	// Missing --new-key-path
	code := runKeysRotateMaster([]string{}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--new-key-path is required") {
		t.Fatalf("stderr = %q, want --new-key-path is required", stderr.String())
	}

	// Bad flag
	stderr.Reset()
	code = runKeysRotateMaster([]string{"--invalid-flag"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

func TestKeysRotateMaster_CurrentKeyRefusals(t *testing.T) {
	tmpDir := t.TempDir()
	validNewKey := writeTestKey(t, tmpDir, "new.key", bytes.Repeat([]byte{0x22}, 32), 0o600)

	// 1. KESTRIX_MASTER_KEY_PATH not set
	withMasterKeyPath(t, "")

	var stdout, stderr bytes.Buffer
	code := runKeysRotateMaster([]string{"--new-key-path", validNewKey}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "KESTRIX_MASTER_KEY_PATH is required") {
		t.Fatalf("stderr = %q, want KESTRIX_MASTER_KEY_PATH is required", stderr.String())
	}

	// 2. Current key file missing
	nonExistent := filepath.Join(tmpDir, "missing.key")
	withMasterKeyPath(t, nonExistent)

	stderr.Reset()
	code = runKeysRotateMaster([]string{"--new-key-path", validNewKey}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "current master key") {
		t.Fatalf("stderr = %q, want current master key error", stderr.String())
	}

	// 3. Current key file wrong permissions (0644)
	badPermKey := writeTestKey(t, tmpDir, "badperm.key", bytes.Repeat([]byte{0x11}, 32), 0o644)
	withMasterKeyPath(t, badPermKey)

	stderr.Reset()
	code = runKeysRotateMaster([]string{"--new-key-path", validNewKey}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "must be mode 0600") {
		t.Fatalf("stderr = %q, want mode 0600 error", stderr.String())
	}

	// 4. Current key file wrong format (too short)
	badFormatKey := writeTestKey(t, tmpDir, "short.key", []byte("too-short"), 0o600)
	withMasterKeyPath(t, badFormatKey)

	stderr.Reset()
	code = runKeysRotateMaster([]string{"--new-key-path", validNewKey}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "expected 32 raw bytes or 64 hex characters") {
		t.Fatalf("stderr = %q, want format error", stderr.String())
	}
}

func TestKeysRotateMaster_NewKeyRefusals(t *testing.T) {
	tmpDir := t.TempDir()
	validOldKey := writeTestKey(t, tmpDir, "old.key", bytes.Repeat([]byte{0x11}, 32), 0o600)
	withMasterKeyPath(t, validOldKey)

	var stdout, stderr bytes.Buffer

	// 1. New key file missing
	code := runKeysRotateMaster([]string{"--new-key-path", filepath.Join(tmpDir, "missing-new.key")}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "new master key") {
		t.Fatalf("stderr = %q, want new master key error", stderr.String())
	}

	// 2. New key file wrong permissions (0644)
	badPermNew := writeTestKey(t, tmpDir, "badperm-new.key", bytes.Repeat([]byte{0x22}, 32), 0o644)
	stderr.Reset()
	code = runKeysRotateMaster([]string{"--new-key-path", badPermNew}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "must be mode 0600") {
		t.Fatalf("stderr = %q, want mode 0600 error", stderr.String())
	}

	// 3. New key file wrong format (too short)
	shortNew := writeTestKey(t, tmpDir, "short-new.key", []byte("too-short"), 0o600)
	stderr.Reset()
	code = runKeysRotateMaster([]string{"--new-key-path", shortNew}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "expected 32 raw bytes or 64 hex characters") {
		t.Fatalf("stderr = %q, want format error", stderr.String())
	}

	// 4. Same key rejected
	sameKey := writeTestKey(t, tmpDir, "same.key", bytes.Repeat([]byte{0x11}, 32), 0o600)
	stderr.Reset()
	code = runKeysRotateMaster([]string{"--new-key-path", sameKey}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "new key must differ from current key") {
		t.Fatalf("stderr = %q, want keys must differ error", stderr.String())
	}
}

func TestKeysRotateMaster_AdvisoryLockRefusal(t *testing.T) {
	dbURL := testUserDBURL(t)
	tmpDir := t.TempDir()
	oldKey := writeTestKey(t, tmpDir, "old.key", bytes.Repeat([]byte{0x11}, 32), 0o600)
	newKey := writeTestKey(t, tmpDir, "new.key", bytes.Repeat([]byte{0x22}, 32), 0o600)

	withMasterKeyPath(t, oldKey)

	// Acquire the server advisory lock beforehand
	pg, err := store.NewPG(dbURL, "local")
	if err != nil {
		t.Skipf("PostgreSQL unavailable: %v", err)
	}
	defer pg.Close()

	ctx := context.Background()
	if err := pg.HoldServerLock(ctx); err != nil {
		t.Fatalf("HoldServerLock: %v", err)
	}
	defer pg.ReleaseServerLock()

	var stdout, stderr bytes.Buffer
	code := runKeysRotateMaster([]string{"--new-key-path", newKey, "--database-url", dbURL}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	wantMsg := "a Kestrix server is connected to this database; stop it first"
	if !strings.Contains(stderr.String(), wantMsg) {
		t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), wantMsg)
	}
}

func TestKeysRotateMaster_SuccessAndOutput(t *testing.T) {
	dbURL := testUserDBURL(t)
	tmpDir := t.TempDir()
	oldKey := writeTestKey(t, tmpDir, "old.key", bytes.Repeat([]byte{0x11}, 32), 0o600)
	newKey := writeTestKey(t, tmpDir, "new.key", bytes.Repeat([]byte{0x22}, 32), 0o600)

	withMasterKeyPath(t, oldKey)

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Skipf("open db: %v", err)
	}
	defer db.Close()
	_, _ = db.Exec(`DELETE FROM remote_jobs`)
	_, _ = db.Exec(`DELETE FROM runs`)
	_, _ = db.Exec(`DELETE FROM credentials`)

	pg, err := store.NewPG(dbURL, "local")
	if err != nil {
		t.Skipf("PostgreSQL unavailable: %v", err)
	}
	defer pg.Close()

	pg.SetCredentialMasterKeyProvider(credentials.FileMasterKeyProvider{Path: oldKey, Version: 1})
	meta, err := pg.CreateCredential(domain.CredentialInput{
		Name:     "cli-cred",
		Type:     "token",
		Provider: "gitlab",
		Host:     "gitlab.com",
		Token:    "glpat-cli-secret",
	})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM credentials WHERE credential_ref = $1`, meta.Ref)
	})

	var stdout, stderr bytes.Buffer
	code := runKeysRotateMaster([]string{"--new-key-path", newKey, "--database-url", dbURL}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0, stderr = %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "Master key rotation completed successfully:") {
		t.Errorf("stdout = %q, want success header", out)
	}
	if !strings.Contains(out, "credentials: 1") {
		t.Errorf("stdout = %q, want credentials: 1", out)
	}
	if !strings.Contains(out, "Replace KESTRIX_MASTER_KEY_PATH with "+newKey+" before starting Kestrix.") {
		t.Errorf("stdout = %q, want operator instruction with new key path", out)
	}

	// Verify decrypt with new key succeeds
	pg.SetCredentialMasterKeyProvider(credentials.FileMasterKeyProvider{Path: newKey, Version: 2})
	cred, err := pg.ResolveCredential(context.Background(), meta.Ref)
	if err != nil || cred.Token != "glpat-cli-secret" {
		t.Fatalf("ResolveCredential with new key: %v, %v", cred, err)
	}
}

func TestCLI_KeysRotateMaster_Subprocess(t *testing.T) {
	dbURL := testUserDBURL(t)
	bin := buildKestrix(t)
	tmpDir := t.TempDir()
	oldKey := writeTestKey(t, tmpDir, "old-sub.key", bytes.Repeat([]byte{0x33}, 32), 0o600)
	newKey := writeTestKey(t, tmpDir, "new-sub.key", bytes.Repeat([]byte{0x44}, 32), 0o600)

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Skipf("open db: %v", err)
	}
	defer db.Close()
	_, _ = db.Exec(`DELETE FROM remote_jobs`)
	_, _ = db.Exec(`DELETE FROM runs`)
	_, _ = db.Exec(`DELETE FROM credentials`)

	t.Setenv("KESTRIX_MASTER_KEY_PATH", oldKey)

	so, se, code := runKestrixStdin(t, bin, "",
		"keys", "rotate-master", "--new-key-path", newKey, "--database-url", dbURL)
	if code != 0 {
		t.Fatalf("exit = %d, stdout=%s stderr=%s", code, so, se)
	}
	if !strings.Contains(so, "Master key rotation completed successfully:") {
		t.Fatalf("stdout = %q, want success header", so)
	}
	if !strings.Contains(so, "Replace KESTRIX_MASTER_KEY_PATH with "+newKey+" before starting Kestrix.") {
		t.Fatalf("stdout = %q, want replacement instruction", so)
	}
}
