package auth

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrSigningKeyUnavailable means the configured JWT signing key could not be
// read or did not pass validation. Error text built around this value must
// never include the key bytes.
var ErrSigningKeyUnavailable = errors.New("token signing key unavailable")

// FileSigningKeyProvider reads a 256-bit HMAC signing key from a file that
// must not be readable by group or other users. It mirrors
// credentials.FileMasterKeyProvider so the two secrets are provisioned the
// same way, without one deriving from the other.
type FileSigningKeyProvider struct {
	Path string
}

// Key reads, validates, and returns the signing key. Every returned error
// wraps ErrSigningKeyUnavailable and names the condition, never the key.
func (p FileSigningKeyProvider) Key() ([]byte, error) {
	if p.Path == "" {
		return nil, fmt.Errorf("%w: KESTRIX_JWT_SIGNING_KEY_PATH is not set", ErrSigningKeyUnavailable)
	}
	info, err := os.Stat(p.Path)
	if err != nil {
		return nil, fmt.Errorf("%w: read signing key: %v", ErrSigningKeyUnavailable, err)
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("%w: signing key file %q must be mode 0600", ErrSigningKeyUnavailable, p.Path)
	}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return nil, fmt.Errorf("%w: read signing key: %v", ErrSigningKeyUnavailable, err)
	}
	return parseSigningKey(data)
}

func parseSigningKey(data []byte) ([]byte, error) {
	value := strings.TrimSpace(string(data))
	if len(value) == 64 {
		if decoded, err := hex.DecodeString(value); err == nil {
			return decoded, nil
		}
	}
	if len(data) == 32 {
		return append([]byte(nil), data...), nil
	}
	return nil, fmt.Errorf("%w: expected 32 raw bytes or 64 hex characters", ErrSigningKeyUnavailable)
}
