// Package auth implements user authentication: password hashing and
// verification, and JWT-based token issuance and verification. It sits
// beside internal/credentials as an internal/domain port adapter.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"

	"kestrix/internal/domain"
)

// ErrInvalidHash means a stored hash could not be parsed. It is returned by
// Verify and never leaks which part of the hash was unreadable.
var ErrInvalidHash = errors.New("auth: invalid password hash")

// Argon2Params are the argon2id cost parameters. They are recorded in every
// hash string this package produces, so raising them later does not
// invalidate hashes already stored.
type Argon2Params struct {
	Time    uint32
	Memory  uint32 // KiB
	Threads uint8
	KeyLen  uint32
	SaltLen uint32
}

// DefaultArgon2Params is the OWASP Password Storage Cheat Sheet's argon2id
// row m=19456 (19 MiB), t=2, p=1: two passes over 19 MiB, single-threaded,
// tuned for an interactive login rather than a batch job. Use this row
// verbatim, or the m=47104,t=1 row, if these parameters ever change - any
// other (memory, iteration) pair is not one OWASP measured.
var DefaultArgon2Params = Argon2Params{
	Time:    2,
	Memory:  19 * 1024,
	Threads: 1,
	KeyLen:  32,
	SaltLen: 16,
}

// Argon2Hasher implements domain.PasswordHasher with argon2id.
type Argon2Hasher struct {
	Params Argon2Params
}

// NewArgon2Hasher returns a hasher using DefaultArgon2Params.
func NewArgon2Hasher() Argon2Hasher {
	return Argon2Hasher{Params: DefaultArgon2Params}
}

var _ domain.PasswordHasher = Argon2Hasher{}

// hashFormat encodes the algorithm, its parameters, and the salt and derived
// key, all required to verify later even if DefaultArgon2Params changes.
const hashFormat = "argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s"

// Hash derives an argon2id hash from password using a fresh random salt and
// the receiver's parameters, and encodes both into the returned string.
func (h Argon2Hasher) Hash(password string) (string, error) {
	p := h.Params
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	encoded := fmt.Sprintf(hashFormat, argon2.Version, p.Memory, p.Time, p.Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))
	return encoded, nil
}

// Verify reports whether password matches hash, using the parameters and
// salt recorded in hash rather than the receiver's own parameters. An
// unparsable hash returns (false, ErrInvalidHash) rather than panicking, so a
// corrupt stored value fails closed.
func (h Argon2Hasher) Verify(hash, password string) (bool, error) {
	params, salt, key, err := decodeHash(hash)
	if err != nil {
		return false, err
	}
	candidate := argon2.IDKey([]byte(password), salt, params.Time, params.Memory, params.Threads, uint32(len(key)))
	return subtle.ConstantTimeCompare(candidate, key) == 1, nil
}

func decodeHash(encoded string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[1], "v=%d", &version); err != nil {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	var memory, time uint32
	var threads uint8
	if n, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil || n != 3 {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	return Argon2Params{Time: time, Memory: memory, Threads: threads, KeyLen: uint32(len(key)), SaltLen: uint32(len(salt))}, salt, key, nil
}

// dummyHash is a fixed, validly-formatted hash with no known matching
// password. VerifyDummy runs the same argon2id work as a real Verify call so
// that authenticating an unknown account costs the same as a wrong password
// on a known one, and neither discloses which case occurred through timing.
var dummyHash = func() string {
	h := NewArgon2Hasher()
	// The password is fixed and irrelevant: this hash is never compared
	// against a real credential, only used to burn equivalent CPU time.
	encoded, err := h.Hash("kestrix-dummy-verification-password")
	if err != nil {
		panic("auth: failed to build dummy hash: " + err.Error())
	}
	return encoded
}()

// VerifyDummy performs a Verify-equivalent amount of work against a fixed
// hash with no matching password, always returning false. Call it in place
// of Verify when the account does not exist, so that account existence is
// not observable through response timing.
func (h Argon2Hasher) VerifyDummy(password string) {
	_, _ = h.Verify(dummyHash, password)
}
