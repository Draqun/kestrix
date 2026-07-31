package credentials

import (
	"bytes"
	"crypto/rand"
	"os"
	"testing"
)

func testProvider() StaticMasterKeyProvider {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return StaticMasterKeyProvider{Version: 1, Keys: map[int][]byte{1: key}}
}

func TestEnvelopeRoundTripAndNoPlaintext(t *testing.T) {
	p := testProvider()
	plain := []byte(`{"token":"super-secret"}`)
	ciphertext, err := Encrypt(p, []byte("credential metadata"), plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte("super-secret")) {
		t.Fatal("ciphertext contains plaintext")
	}
	got, err := Decrypt(p, []byte("credential metadata"), ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("got %q, want %q", got, plain)
	}
}

func TestEnvelopeRejectsTamperingAndWrongAAD(t *testing.T) {
	p := testProvider()
	ciphertext, err := Encrypt(p, []byte("metadata"), []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[len(ciphertext)-2] ^= 1
	if _, err := Decrypt(p, []byte("metadata"), ciphertext); err == nil {
		t.Fatal("expected tampering error")
	}
	ciphertext, _ = Encrypt(p, []byte("metadata"), []byte("secret"))
	if _, err := Decrypt(p, []byte("other metadata"), ciphertext); err == nil {
		t.Fatal("expected authenticated metadata error")
	}
}

func TestEnvelopeRewrap(t *testing.T) {
	old := testProvider()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	newKeys := StaticMasterKeyProvider{Version: 2, Keys: map[int][]byte{2: key}}
	ciphertext, err := Encrypt(old, []byte("metadata"), []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	rewrapped, err := Rewrap(old, newKeys, []byte("metadata"), ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(old, []byte("metadata"), rewrapped); err == nil {
		t.Fatal("old key unexpectedly decrypted rewrapped data")
	}
	plain, err := Decrypt(newKeys, []byte("metadata"), rewrapped)
	if err != nil || string(plain) != "secret" {
		t.Fatalf("plain=%q err=%v", plain, err)
	}
}

func TestKeyProvidersAndUnavailableKeys(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/master.key"
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		t.Fatal(err)
	}
	file := FileMasterKeyProvider{Path: path, Version: 1}
	if _, got, err := file.Current(); err != nil || len(got) != 32 {
		t.Fatalf("file key err=%v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Key(1); err == nil {
		t.Fatal("expected insecure file error")
	}
	if err := os.WriteFile(path, []byte("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Key(1); err != nil {
		t.Fatal(err)
	}
	keyring := OSKeyringProvider{Lookup: func(string, string) ([]byte, error) { return key, nil }}
	if _, _, err := keyring.Current(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := (StaticMasterKeyProvider{Version: 1, Keys: map[int][]byte{}}).Current(); err == nil {
		t.Fatal("expected unavailable key")
	}
	if _, err := (FileMasterKeyProvider{Path: dir + "/missing", Version: 1}).Key(1); err == nil {
		t.Fatal("expected missing file error")
	}
	badRing := OSKeyringProvider{Lookup: func(string, string) ([]byte, error) { return nil, os.ErrNotExist }}
	if _, err := badRing.Key(1); err == nil {
		t.Fatal("expected keyring lookup error")
	}
	if _, err := (OSKeyringProvider{}).Key(1); err == nil {
		t.Fatal("expected unavailable default keyring")
	}
	if _, err := parseKey([]byte("not-a-key")); err == nil {
		t.Fatal("expected invalid key error")
	}
	if err := os.Chmod(dir, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (FileMasterKeyProvider{Path: dir, Version: 1}).Key(1); err == nil {
		t.Fatal("expected directory read error")
	}
	_ = os.Chmod(dir, 0o700)
}

func TestEnvelopeMalformedAndProviderErrors(t *testing.T) {
	provider := testProvider()
	if _, err := Decrypt(provider, nil, []byte("bad")); err == nil {
		t.Fatal("expected malformed envelope")
	}
	if _, err := Encrypt(StaticMasterKeyProvider{Version: 1, Keys: map[int][]byte{}}, nil, []byte("x")); err == nil {
		t.Fatal("expected encrypt key error")
	}
	if _, err := Rewrap(StaticMasterKeyProvider{Version: 1, Keys: map[int][]byte{}}, provider, nil, []byte("bad")); err == nil {
		t.Fatal("expected rewrap error")
	}
}
