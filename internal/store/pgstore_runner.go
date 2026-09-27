package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"kestrix/internal/domain"
)

// newRunnerToken generates a random, URL-safe bearer token for a runner.
// The plaintext is returned to the caller exactly once; only its hash is
// ever stored - see design.md D4.
func newRunnerToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate runner token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func hashRunnerToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateRunner registers a new runner and returns its ID and a newly
// generated bearer token, returned once in plain text - only its hash is
// persisted.
func (s *PGStore) CreateRunner(name string, tags []string) (int, string, error) {
	token, err := newRunnerToken()
	if err != nil {
		return 0, "", err
	}
	var id int
	err = s.db.QueryRow(
		`INSERT INTO runners (name, token_hash, tags) VALUES ($1, $2, $3) RETURNING id`,
		name, hashRunnerToken(token), nonNilTags(tags),
	).Scan(&id)
	if err != nil {
		return 0, "", err
	}
	return id, token, nil
}

// AuthenticateRunner resolves token to a runner ID. A stored token is a
// SHA-256 hash of a 256-bit random value, so an indexed lookup on the hash
// carries no timing side channel on the token itself - the same shape a
// hashed-and-looked-up bearer credential always uses. ok is false for an
// unknown or revoked token.
func (s *PGStore) AuthenticateRunner(token string) (int, bool, error) {
	var id int
	err := s.db.QueryRow(
		`UPDATE runners SET last_seen_at = now() WHERE token_hash = $1 AND revoked_at IS NULL RETURNING id`,
		hashRunnerToken(token),
	).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// RevokeRunner marks a runner revoked; its token stops authenticating.
func (s *PGStore) RevokeRunner(id int) error {
	_, err := s.db.Exec(`UPDATE runners SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id)
	return err
}

// nonNilTags turns a nil slice into an empty one, so it binds as '{}' and
// never NULL (the column is NOT NULL).
func nonNilTags(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

// ListRunners returns every unrevoked runner, oldest first, with whether it
// currently holds a claimed or running job.
func (s *PGStore) ListRunners() ([]domain.Runner, error) {
	rows, err := s.db.Query(
		`SELECT r.id, r.name, array_to_json(r.tags)::text, r.created_at, r.last_seen_at,
		        EXISTS (SELECT 1 FROM remote_jobs j WHERE j.runner_id = r.id AND j.status IN ($1, $2))
		 FROM runners r WHERE r.revoked_at IS NULL ORDER BY r.id`,
		domain.RemoteJobStatusClaimed, domain.RemoteJobStatusRunning,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runners []domain.Runner
	for rows.Next() {
		var r domain.Runner
		var lastSeen sql.NullTime
		var tagsJSON string // database/sql cannot scan TEXT[] into []string
		if err := rows.Scan(&r.ID, &r.Name, &tagsJSON, &r.CreatedAt, &lastSeen, &r.Busy); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(tagsJSON), &r.Tags); err != nil {
			return nil, fmt.Errorf("decode runner tags: %w", err)
		}
		if lastSeen.Valid {
			r.LastSeenAt = &lastSeen.Time
		}
		runners = append(runners, r)
	}
	return runners, rows.Err()
}
