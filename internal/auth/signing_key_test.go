package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSigningKeyProvider_ValidRawKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jwt.key")
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := (FileSigningKeyProvider{Path: path}).Key()
	if err != nil {
		t.Fatalf("Key() error = %v", err)
	}
	if len(got) != 32 {
		t.Fatalf("Key() length = %d, want 32", len(got))
	}
}

func TestFileSigningKeyProvider_ValidHexKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jwt.key")
	hexKey := "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	if err := os.WriteFile(path, []byte(hexKey), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := (FileSigningKeyProvider{Path: path}).Key()
	if err != nil {
		t.Fatalf("Key() error = %v", err)
	}
	if len(got) != 32 {
		t.Fatalf("Key() length = %d, want 32", len(got))
	}
}

func TestFileSigningKeyProvider_Unset(t *testing.T) {
	if _, err := (FileSigningKeyProvider{Path: ""}).Key(); err == nil {
		t.Fatal("expected error for unset path")
	}
}

func TestFileSigningKeyProvider_MissingFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := (FileSigningKeyProvider{Path: filepath.Join(dir, "missing")}).Key(); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestFileSigningKeyProvider_UnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file permissions do not block reads")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "jwt.key")
	if err := os.WriteFile(path, make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0o600)
	if _, err := (FileSigningKeyProvider{Path: path}).Key(); err == nil {
		t.Fatal("expected error for unreadable file")
	}
}

func TestFileSigningKeyProvider_PermissiveFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jwt.key")
	if err := os.WriteFile(path, make([]byte, 32), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (FileSigningKeyProvider{Path: path}).Key(); err == nil {
		t.Fatal("expected error for group/other-readable file")
	}
}

func TestFileSigningKeyProvider_MalformedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jwt.key")
	if err := os.WriteFile(path, []byte("not-a-valid-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (FileSigningKeyProvider{Path: path}).Key(); err == nil {
		t.Fatal("expected error for malformed key content")
	}
}

func TestFileSigningKeyProvider_ErrorNeverIncludesKeyBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jwt.key")
	secret := "super-secret-key-material-do-not-leak-this-value"
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := (FileSigningKeyProvider{Path: path}).Key()
	if err == nil {
		t.Fatal("expected error for malformed key content")
	}
	if got := err.Error(); strings.Contains(got, secret) {
		t.Fatalf("error leaked key material: %s", got)
	}
}
