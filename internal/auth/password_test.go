package auth

import (
	"strings"
	"testing"
	"time"
)

func TestArgon2Hasher_HashAndVerify(t *testing.T) {
	h := NewArgon2Hasher()
	hash, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	ok, err := h.Verify(hash, "correct horse battery staple")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatal("Verify() = false, want true for the correct password")
	}
	ok, err = h.Verify(hash, "wrong password")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ok {
		t.Fatal("Verify() = true, want false for a wrong password")
	}
}

func TestArgon2Hasher_SamePasswordDifferentHashes(t *testing.T) {
	h := NewArgon2Hasher()
	hash1, err := h.Hash("same-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	hash2, err := h.Hash("same-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if hash1 == hash2 {
		t.Fatal("two hashes of the same password are identical; salts are not random")
	}
	for _, h2 := range []string{hash1, hash2} {
		ok, err := h.Verify(h2, "same-password")
		if err != nil || !ok {
			t.Fatalf("Verify(%q) = %v, %v, want true, nil", h2, ok, err)
		}
	}
}

func TestArgon2Hasher_StoredParametersHonored(t *testing.T) {
	// Hash with parameters different from the current default, then verify
	// using a hasher whose default parameters differ - the hash's own
	// recorded parameters must be used, not the hasher's.
	oldParams := Argon2Params{Time: 2, Memory: 8 * 1024, Threads: 1, KeyLen: 32, SaltLen: 16}
	old := Argon2Hasher{Params: oldParams}
	hash, err := old.Hash("legacy-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.Contains(hash, "m=8192,t=2,p=1") {
		t.Fatalf("hash does not record its parameters: %s", hash)
	}

	current := Argon2Hasher{Params: Argon2Params{Time: 1, Memory: 19 * 1024, Threads: 1, KeyLen: 32, SaltLen: 16}}
	ok, err := current.Verify(hash, "legacy-password")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatal("Verify() = false, want true when the hash's own parameters differ from the hasher's defaults")
	}
}

func TestArgon2Hasher_Verify_InvalidHash(t *testing.T) {
	h := NewArgon2Hasher()
	for _, bad := range []string{
		"",                                      // too few fields
		"not-a-hash",                            // too few fields
		"argon2id$v=19$m=x,t=1,p=1$c2FsdA$a2V5", // malformed m=/t=/p= segment
		"argon2id$v=abc$m=19456,t=1,p=1$c2FsdA$a2V5",       // malformed v= segment
		"argon2id$v=19$m=19456,t=1,p=1$not-valid!!$a2V5",   // malformed salt base64
		"argon2id$v=19$m=19456,t=1,p=1$c2FsdA$not-valid!!", // malformed key base64
	} {
		if _, err := h.Verify(bad, "anything"); err == nil {
			t.Errorf("Verify(%q, ...) returned nil error, want ErrInvalidHash", bad)
		}
	}
}

func TestArgon2Hasher_VerifyDummy_RunsForMissingAccount(t *testing.T) {
	h := NewArgon2Hasher()

	realHash, err := h.Hash("some-real-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	timeReal := timeIt(func() { _, _ = h.Verify(realHash, "wrong-guess") })
	timeDummy := timeIt(func() { h.VerifyDummy("wrong-guess") })

	// Both paths run one argon2id derivation; neither should be
	// order-of-magnitude cheaper than the other. A generous ratio avoids
	// flaking on a loaded CI machine while still catching a VerifyDummy
	// that short-circuits without doing the work.
	ratio := float64(timeDummy) / float64(timeReal)
	if ratio < 0.2 || ratio > 5 {
		t.Fatalf("VerifyDummy took %v, real Verify took %v (ratio %.2f); expected comparable cost", timeDummy, timeReal, ratio)
	}
}

func timeIt(fn func()) time.Duration {
	start := time.Now()
	fn()
	return time.Since(start)
}
