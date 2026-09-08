package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"kestrix/internal/domain"
)

// ErrUserNotFound is returned by user lookups for a name with no account.
var ErrUserNotFound = errors.New("user not found")

// ErrUserExists is returned by CreateUser when the name is already taken.
var ErrUserExists = errors.New("user already exists")

const pgUniqueViolation = "23505"

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}

// CreateUser persists a new account. It never overwrites an existing name;
// see ErrUserExists.
func (s *PGStore) CreateUser(name string, role domain.Role, passwordHash string) (domain.User, error) {
	var u domain.User
	u.Name = name
	u.Role = role
	err := s.db.QueryRow(
		`INSERT INTO users (name, password_hash, role) VALUES ($1, $2, $3) RETURNING token_version, created_at, updated_at`,
		name, passwordHash, string(role),
	).Scan(&u.TokenVersion, &u.CreatedAt, &u.UpdatedAt)
	if isUniqueViolation(err) {
		return domain.User{}, fmt.Errorf("%w: %s", ErrUserExists, name)
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// GetUser reads one account without its password hash.
func (s *PGStore) GetUser(name string) (domain.User, error) {
	var u domain.User
	var role string
	err := s.db.QueryRow(
		`SELECT name, role, token_version, created_at, updated_at FROM users WHERE name = $1`, name,
	).Scan(&u.Name, &role, &u.TokenVersion, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, fmt.Errorf("%w: %s", ErrUserNotFound, name)
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("read user: %w", err)
	}
	u.Role = domain.Role(role)
	return u, nil
}

// GetUserCredential reads one account with its password hash, for
// authentication only. Callers must never return this value from an API
// response or CLI output.
func (s *PGStore) GetUserCredential(name string) (domain.UserCredential, error) {
	var uc domain.UserCredential
	var role string
	err := s.db.QueryRow(
		`SELECT name, role, token_version, created_at, updated_at, password_hash FROM users WHERE name = $1`, name,
	).Scan(&uc.Name, &role, &uc.TokenVersion, &uc.CreatedAt, &uc.UpdatedAt, &uc.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.UserCredential{}, fmt.Errorf("%w: %s", ErrUserNotFound, name)
	}
	if err != nil {
		return domain.UserCredential{}, fmt.Errorf("read user credential: %w", err)
	}
	uc.Role = domain.Role(role)
	return uc, nil
}

// ListUsers returns every account without password hashes, ordered by name.
func (s *PGStore) ListUsers() ([]domain.User, error) {
	rows, err := s.db.Query(`SELECT name, role, token_version, created_at, updated_at FROM users ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var result []domain.User
	for rows.Next() {
		var u domain.User
		var role string
		if err := rows.Scan(&u.Name, &role, &u.TokenVersion, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		u.Role = domain.Role(role)
		result = append(result, u)
	}
	return result, rows.Err()
}

// UpdateUserRole changes an account's global role and bumps its token
// version in the same statement, so a token issued before the change stops
// verifying on its next use; see domain.UserStore.
func (s *PGStore) UpdateUserRole(name string, role domain.Role) error {
	result, err := s.db.Exec(
		`UPDATE users SET role = $2, token_version = token_version + 1, updated_at = now() WHERE name = $1`,
		name, string(role),
	)
	if err != nil {
		return fmt.Errorf("update user role: %w", err)
	}
	return requireRowAffected(result, ErrUserNotFound, name)
}

// UpdateUserPassword replaces an account's stored password hash and bumps
// its token version in the same statement, ending any session issued before
// the change.
func (s *PGStore) UpdateUserPassword(name string, passwordHash string) error {
	result, err := s.db.Exec(
		`UPDATE users SET password_hash = $2, token_version = token_version + 1, updated_at = now() WHERE name = $1`,
		name, passwordHash,
	)
	if err != nil {
		return fmt.Errorf("update user password: %w", err)
	}
	return requireRowAffected(result, ErrUserNotFound, name)
}

// BumpTokenVersion invalidates every token already issued for name without
// changing its role or password.
func (s *PGStore) BumpTokenVersion(name string) error {
	result, err := s.db.Exec(
		`UPDATE users SET token_version = token_version + 1, updated_at = now() WHERE name = $1`, name,
	)
	if err != nil {
		return fmt.Errorf("bump user token version: %w", err)
	}
	return requireRowAffected(result, ErrUserNotFound, name)
}

// DeleteUser removes an account. Its project grants are removed with it via
// the project_members foreign key's ON DELETE CASCADE.
func (s *PGStore) DeleteUser(name string) error {
	result, err := s.db.Exec(`DELETE FROM users WHERE name = $1`, name)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return requireRowAffected(result, ErrUserNotFound, name)
}

// requireRowAffected returns wrapped after wrapping it with id when result
// reports zero rows affected, and nil otherwise.
func requireRowAffected(result sql.Result, wrapped error, id string) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", wrapped, id)
	}
	return nil
}
