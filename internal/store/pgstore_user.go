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

// ErrSetupDone is returned by CreateFirstUser once any account already
// exists: first-run setup only ever creates one account.
var ErrSetupDone = errors.New("setup already completed")

const pgUniqueViolation = "23505"

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}

// userQuerier is satisfied by both *sql.DB and *sql.Tx, letting insertUser
// run either directly or inside a transaction.
type userQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

// insertUser runs the INSERT shared by CreateUser and CreateFirstUser, so
// the statement exists in exactly one place regardless of which one calls
// it or whether it runs inside a transaction.
func insertUser(q userQuerier, name string, role domain.Role, passwordHash string) (domain.User, error) {
	var u domain.User
	u.Name = name
	u.Role = role
	err := q.QueryRow(
		`INSERT INTO users (name, password_hash, role) VALUES ($1, $2, $3) RETURNING token_version, created_at, updated_at`,
		name, passwordHash, string(role),
	).Scan(&u.TokenVersion, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return domain.User{}, err
	}
	return u, nil
}

// CreateUser persists a new account. It never overwrites an existing name;
// see ErrUserExists.
func (s *PGStore) CreateUser(name string, role domain.Role, passwordHash string) (domain.User, error) {
	u, err := insertUser(s.db, name, role, passwordHash)
	if isUniqueViolation(err) {
		return domain.User{}, fmt.Errorf("%w: %s", ErrUserExists, name)
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// CreateFirstUser creates the first account with role, but only while the
// users table is empty; it returns ErrSetupDone otherwise, without creating,
// modifying, or deleting anything. The emptiness check and the insert run in
// one transaction: LOCK TABLE serializes concurrent callers so exactly one
// observes an empty table and inserts; see design.md D1.
func (s *PGStore) CreateFirstUser(name string, role domain.Role, passwordHash string) (domain.User, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return domain.User{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return domain.User{}, fmt.Errorf("lock users: %w", err)
	}
	var count int
	if err := tx.QueryRow(`SELECT count(*) FROM users`).Scan(&count); err != nil {
		return domain.User{}, fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return domain.User{}, ErrSetupDone
	}

	u, err := insertUser(tx, name, role, passwordHash)
	if err != nil {
		return domain.User{}, fmt.Errorf("create first user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.User{}, fmt.Errorf("commit create first user: %w", err)
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

// HasAnyUser reports whether the users table has at least one row, without
// fetching any row's data - see domain.UserStore.
func (s *PGStore) HasAnyUser() (bool, error) {
	var exists bool
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users)`).Scan(&exists); err != nil {
		return false, fmt.Errorf("check for any user: %w", err)
	}
	return exists, nil
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
