package store

import (
	"database/sql"
	"errors"
	"fmt"

	"kestrix/internal/domain"
)

// ErrGrantNotFound is returned by RemoveProjectGrant for a pair with no grant.
var ErrGrantNotFound = errors.New("project grant not found")

// SetProjectGrant creates or replaces the role a user holds on a project.
func (s *PGStore) SetProjectGrant(userName, projectName string, role domain.Role) error {
	_, err := s.db.Exec(
		`INSERT INTO project_members (user_name, project_name, role) VALUES ($1, $2, $3)
		 ON CONFLICT (user_name, project_name) DO UPDATE SET role = EXCLUDED.role`,
		userName, projectName, string(role),
	)
	if err != nil {
		return fmt.Errorf("set project grant: %w", err)
	}
	return nil
}

// RemoveProjectGrant deletes one user's grant on one project.
func (s *PGStore) RemoveProjectGrant(userName, projectName string) error {
	result, err := s.db.Exec(
		`DELETE FROM project_members WHERE user_name = $1 AND project_name = $2`,
		userName, projectName,
	)
	if err != nil {
		return fmt.Errorf("remove project grant: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %s/%s", ErrGrantNotFound, userName, projectName)
	}
	return nil
}

// ListGrantsForUser returns every project grant held by userName, ordered by
// project name.
func (s *PGStore) ListGrantsForUser(userName string) ([]domain.ProjectGrant, error) {
	return s.queryGrants(`SELECT user_name, project_name, role FROM project_members WHERE user_name = $1 ORDER BY project_name`, userName)
}

// ListGrantsForProject returns every grant held on projectName, ordered by
// user name.
func (s *PGStore) ListGrantsForProject(projectName string) ([]domain.ProjectGrant, error) {
	return s.queryGrants(`SELECT user_name, project_name, role FROM project_members WHERE project_name = $1 ORDER BY user_name`, projectName)
}

func (s *PGStore) queryGrants(query string, arg string) ([]domain.ProjectGrant, error) {
	rows, err := s.db.Query(query, arg)
	if err != nil {
		return nil, fmt.Errorf("list project grants: %w", err)
	}
	defer rows.Close()
	var result []domain.ProjectGrant
	for rows.Next() {
		var g domain.ProjectGrant
		var role string
		if err := rows.Scan(&g.UserName, &g.ProjectName, &role); err != nil {
			return nil, fmt.Errorf("scan project grant: %w", err)
		}
		g.Role = domain.Role(role)
		result = append(result, g)
	}
	return result, rows.Err()
}

// ResolveGrant reports the role userName holds on projectName, and whether a
// grant exists at all - distinct from an existing grant at the zero Role,
// which this schema never stores.
func (s *PGStore) ResolveGrant(userName, projectName string) (domain.Role, bool, error) {
	var role string
	err := s.db.QueryRow(
		`SELECT role FROM project_members WHERE user_name = $1 AND project_name = $2`,
		userName, projectName,
	).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("resolve project grant: %w", err)
	}
	return domain.Role(role), true, nil
}
