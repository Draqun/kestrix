package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"sync"

	"kestrix/internal/credentials"
	"kestrix/internal/domain"
)

// ServerAdvisoryLockKey is the PostgreSQL advisory lock key held by a running
// Kestrix server for its process lifetime, preventing offline administrative
// operations (like master-key rotation) from mutating encrypted rows while the
// server is running.
const ServerAdvisoryLockKey int64 = 4711002

// ErrMasterKeyMismatch is returned at startup when the configured master key
// cannot decrypt stored credentials.
var ErrMasterKeyMismatch = errors.New("the configured master key does not decrypt stored credentials; restore the previous key or complete a rotation with 'kestrix keys rotate-master'")

// HoldServerLock acquires the session-level advisory lock on a dedicated
// connection and retains it until ReleaseServerLock or Close is called.
func (s *PGStore) HoldServerLock(ctx context.Context) error {
	if s.serverLockMu != nil {
		s.serverLockMu.Lock()
		defer s.serverLockMu.Unlock()
	}

	if s.serverLockConn != nil {
		return nil
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open dedicated server lock connection: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", ServerAdvisoryLockKey); err != nil {
		_ = conn.Close()
		return fmt.Errorf("acquire server advisory lock: %w", err)
	}
	s.serverLockConn = conn
	return nil
}

// ReleaseServerLock releases the server advisory lock and closes the dedicated
// connection.
func (s *PGStore) ReleaseServerLock() error {
	if s.serverLockMu != nil {
		s.serverLockMu.Lock()
		defer s.serverLockMu.Unlock()
	}

	if s.serverLockConn == nil {
		return nil
	}
	conn := s.serverLockConn
	s.serverLockConn = nil
	_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", ServerAdvisoryLockKey)
	return conn.Close()
}

// TryServerLock attempts to acquire the server advisory lock. If acquired, it
// returns true and an io.Closer that releases the lock and closes the
// connection. If another session holds the lock, it returns false, nil, nil.
func (s *PGStore) TryServerLock(ctx context.Context) (bool, io.Closer, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("open connection: %w", err)
	}
	var locked bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", ServerAdvisoryLockKey).Scan(&locked); err != nil {
		_ = conn.Close()
		return false, nil, fmt.Errorf("try advisory lock: %w", err)
	}
	if !locked {
		_ = conn.Close()
		return false, nil, nil
	}
	return true, &serverLockCloser{conn: conn}, nil
}

type serverLockCloser struct {
	mu   sync.Mutex
	conn *sql.Conn
}

func (c *serverLockCloser) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	conn := c.conn
	c.conn = nil
	_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", ServerAdvisoryLockKey)
	return conn.Close()
}

// CheckMasterKey verifies that the configured master key can decrypt at least one
// stored credential row, if any credential exists. With no stored credentials,
// it returns nil. If decryption fails, it returns ErrMasterKeyMismatch.
func (s *PGStore) CheckMasterKey(provider credentials.MasterKeyProvider) error {
	if provider == nil {
		return credentials.ErrMasterKeyUnavailable
	}
	var (
		ref      string
		name     string
		credType string
		prov     string
		host     string
		payload  []byte
	)
	err := s.db.QueryRow(`SELECT credential_ref, name, credential_type, provider, host, encrypted_payload FROM credentials LIMIT 1`).
		Scan(&ref, &name, &credType, &prov, &host, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query credential for master key check: %w", err)
	}
	meta := domain.CredentialMetadata{
		Ref:      ref,
		Name:     name,
		Type:     credType,
		Provider: prov,
		Host:     host,
	}
	if _, err := credentials.Decrypt(provider, credentialAAD(meta), payload); err != nil {
		return ErrMasterKeyMismatch
	}
	return nil
}

// Close closes any active server lock connection and the underlying database.
func (s *PGStore) Close() error {
	_ = s.ReleaseServerLock()
	if s.db != nil {
		err := s.db.Close()
		s.db = nil
		return err
	}
	return nil
}
