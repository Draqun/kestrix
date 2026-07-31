// Package credentials contains provider-neutral credential lifecycle support.
package credentials

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var ErrMasterKeyUnavailable = errors.New("credential master key unavailable")

// MasterKeyProvider supplies versioned wrapping keys. Implementations must
// keep the key outside PostgreSQL and outside pipeline/run data.
type MasterKeyProvider interface {
	Current() (version int, key []byte, err error)
	Key(version int) ([]byte, error)
}

// StaticMasterKeyProvider is useful for tests and controlled embedding.
type StaticMasterKeyProvider struct {
	Version int
	Keys    map[int][]byte
}

func (p StaticMasterKeyProvider) Current() (int, []byte, error) {
	key, err := p.Key(p.Version)
	return p.Version, key, err
}

func (p StaticMasterKeyProvider) Key(version int) ([]byte, error) {
	key := p.Keys[version]
	if len(key) != 32 {
		return nil, fmt.Errorf("%w: key version %d is not a 256-bit key", ErrMasterKeyUnavailable, version)
	}
	return append([]byte(nil), key...), nil
}

// FileMasterKeyProvider reads a raw 32-byte or hex-encoded 256-bit key from a
// 0600-mounted file. The file path is configuration, never a secret value.
type FileMasterKeyProvider struct {
	Path    string
	Version int
}

func (p FileMasterKeyProvider) Current() (int, []byte, error) {
	key, err := p.Key(p.Version)
	return p.Version, key, err
}

func (p FileMasterKeyProvider) Key(_ int) ([]byte, error) {
	info, err := os.Stat(p.Path)
	if err != nil {
		return nil, fmt.Errorf("%w: read master key: %v", ErrMasterKeyUnavailable, err)
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("%w: master key file %q must be mode 0600", ErrMasterKeyUnavailable, p.Path)
	}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return nil, fmt.Errorf("%w: read master key: %v", ErrMasterKeyUnavailable, err)
	}
	return parseKey(data)
}

// OSKeyringProvider uses the standard secret-tool CLI when available. The
// provider can be injected with Lookup in tests or by another OS integration.
type OSKeyringProvider struct {
	Service string
	Account string
	Version int
	Lookup  func(service, account string) ([]byte, error)
}

func (p OSKeyringProvider) Current() (int, []byte, error) {
	key, err := p.Key(p.Version)
	return p.Version, key, err
}

func (p OSKeyringProvider) Key(_ int) ([]byte, error) {
	service, account := p.Service, p.Account
	if service == "" {
		service = "kestrix"
	}
	if account == "" {
		account = "master-key"
	}
	if p.Lookup != nil {
		key, err := p.Lookup(service, account)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMasterKeyUnavailable, err)
		}
		return parseKey(key)
	}
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return nil, fmt.Errorf("%w: OS keyring secret-tool is unavailable", ErrMasterKeyUnavailable)
	}
	out, err := exec.Command("secret-tool", "lookup", "service", service, "account", account).Output()
	if err != nil {
		return nil, fmt.Errorf("%w: lookup OS keyring: %v", ErrMasterKeyUnavailable, err)
	}
	return parseKey(out)
}

func parseKey(data []byte) ([]byte, error) {
	value := strings.TrimSpace(string(data))
	if len(value) == 64 {
		if decoded, err := hex.DecodeString(value); err == nil {
			return decoded, nil
		}
	}
	if len(data) == 32 {
		return append([]byte(nil), data...), nil
	}
	return nil, fmt.Errorf("%w: expected 32 raw bytes or 64 hex characters", ErrMasterKeyUnavailable)
}
