package store

import (
	"database/sql"
	"fmt"
	"time"

	"kestrix/internal/source"
)

func (s *PGStore) Load() (map[string]*ProjectConfig, error) {
	rows, err := s.db.Query(`SELECT name, repo_url, repo_ref, COALESCE(repo_credential_ref,''), pipeline_dir, pipeline_file, gitlab_project_id, default_branch, webhook_secret, mode, created_at FROM projects`)
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()

	projects := make(map[string]*ProjectConfig)
	for rows.Next() {
		var p ProjectConfig
		var createdAt time.Time
		if err := rows.Scan(&p.Name, &p.RepoURL, &p.RepoRef, &p.RepoCredentialRef, &p.PipelineDir, &p.PipelineFile, &p.GitLabProjectID, &p.DefaultBranch, &p.WebhookSecret, &p.Mode, &createdAt); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		p.CreatedAt = createdAt.Format(time.RFC3339)
		projects[p.Name] = &p
	}
	return projects, rows.Err()
}

func (s *PGStore) Save(projects map[string]*ProjectConfig) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM projects`); err != nil {
		return fmt.Errorf("clear projects: %w", err)
	}

	for _, p := range projects {
		mode := p.Mode
		if mode == "" {
			mode = s.defaultMode
		}
		if _, err := tx.Exec(
			`INSERT INTO projects (name, repo_url, repo_ref, repo_credential_ref, pipeline_dir, pipeline_file, gitlab_project_id, default_branch, webhook_secret, mode, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			p.Name, p.RepoURL, p.RepoRef, nullIfEmpty(p.RepoCredentialRef), p.PipelineDir, p.PipelineFile, p.GitLabProjectID, p.DefaultBranch, p.WebhookSecret, mode, p.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert %s: %w", p.Name, err)
		}
	}
	return tx.Commit()
}

func (s *PGStore) Get(name string) (*ProjectConfig, error) {
	var p ProjectConfig
	var createdAt time.Time
	err := s.db.QueryRow(
		`SELECT name, repo_url, repo_ref, COALESCE(repo_credential_ref,''), pipeline_dir, pipeline_file, gitlab_project_id, default_branch, webhook_secret, mode, created_at FROM projects WHERE name = $1`, name,
	).Scan(&p.Name, &p.RepoURL, &p.RepoRef, &p.RepoCredentialRef, &p.PipelineDir, &p.PipelineFile, &p.GitLabProjectID, &p.DefaultBranch, &p.WebhookSecret, &p.Mode, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get project %s: %w", name, err)
	}
	p.CreatedAt = createdAt.Format(time.RFC3339)
	if p.Sources, err = s.loadSources(name); err != nil {
		return nil, err
	}
	return &p, nil
}

// loadSourcesFor returns the derived sources of every named project, each
// list primary first, in one query.
func (s *PGStore) loadSourcesFor(names []string) (map[string][]ProjectSource, error) {
	out := make(map[string][]ProjectSource, len(names))
	if len(names) == 0 {
		return out, nil
	}
	// COLLATE "C" pins byte order, which is what service.DeriveSources
	// produces. Without it the order follows the database's collation, and on
	// a glibc-collated database (the Debian image, RDS, Cloud SQL) a name with
	// mixed case or punctuation sorts differently than in Go - so the web
	// package's refreshSources would see an unchanged definition as changed and
	// rewrite project_sources on every delivery. musl-based images happen to
	// collate byte-wise, which is why the default hides this locally.
	rows, err := s.db.Query(`SELECT project_name, source_name, repo_url, is_primary FROM project_sources
		WHERE project_name = ANY($1)
		ORDER BY project_name, is_primary DESC, source_name COLLATE "C", repo_url COLLATE "C"`, names)
	if err != nil {
		return nil, fmt.Errorf("load sources: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var src ProjectSource
		if err := rows.Scan(&name, &src.Name, &src.URL, &src.Primary); err != nil {
			return nil, fmt.Errorf("scan source: %w", err)
		}
		out[name] = append(out[name], src)
	}
	return out, rows.Err()
}

// loadSources returns one project's derived sources, primary first.
func (s *PGStore) loadSources(name string) ([]ProjectSource, error) {
	byName, err := s.loadSourcesFor([]string{name})
	if err != nil {
		return nil, err
	}
	return byName[name], nil
}

// ReplaceSources deletes and re-inserts a project's sources in one
// transaction. Two sources with the same match key collapse to the first.
func (s *PGStore) ReplaceSources(name string, sources []ProjectSource) error {
	return s.inTx("replace sources for "+name, func(tx *sql.Tx) error {
		return replaceSourcesTx(tx, name, sources)
	})
}

// FindBySourceKey returns the distinct projects declaring any of keys as a
// source, ordered by name, each with its full source set. It runs before the
// delivery is verified, so its cost is two queries whatever the number of
// matching projects: an unauthenticated URL must not be able to multiply
// database work.
func (s *PGStore) FindBySourceKey(keys []string) ([]*ProjectConfig, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT DISTINCT p.name, p.repo_url, p.repo_ref, COALESCE(p.repo_credential_ref,''), p.pipeline_dir, p.pipeline_file, p.gitlab_project_id, p.default_branch, p.webhook_secret, p.mode, p.created_at
		FROM projects p JOIN project_sources s ON s.project_name = p.name WHERE s.match_key = ANY($1) ORDER BY p.name`, keys)
	if err != nil {
		return nil, fmt.Errorf("find by source key: %w", err)
	}
	var projects []*ProjectConfig
	var names []string
	for rows.Next() {
		var p ProjectConfig
		var createdAt time.Time
		if err := rows.Scan(&p.Name, &p.RepoURL, &p.RepoRef, &p.RepoCredentialRef, &p.PipelineDir, &p.PipelineFile, &p.GitLabProjectID, &p.DefaultBranch, &p.WebhookSecret, &p.Mode, &createdAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan matched project: %w", err)
		}
		p.CreatedAt = createdAt.Format(time.RFC3339)
		projects = append(projects, &p)
		names = append(names, p.Name)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	sources, err := s.loadSourcesFor(names)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		p.Sources = sources[p.Name]
	}
	return projects, nil
}

func (s *PGStore) FindByGitLabID(id int) (*ProjectConfig, error) {
	var p ProjectConfig
	var createdAt time.Time
	err := s.db.QueryRow(
		`SELECT name, repo_url, repo_ref, COALESCE(repo_credential_ref,''), pipeline_dir, pipeline_file, gitlab_project_id, default_branch, webhook_secret, mode, created_at FROM projects WHERE gitlab_project_id = $1`, id,
	).Scan(&p.Name, &p.RepoURL, &p.RepoRef, &p.RepoCredentialRef, &p.PipelineDir, &p.PipelineFile, &p.GitLabProjectID, &p.DefaultBranch, &p.WebhookSecret, &p.Mode, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find by gitlab id %d: %w", id, err)
	}
	p.CreatedAt = createdAt.Format(time.RFC3339)
	// Populated like Get and FindBySourceKey: the webhook path compares this
	// set with the derived one to skip an unnecessary write, and a nil set
	// would always compare unequal. One extra query, independent of the
	// payload, so it cannot be multiplied by an unauthenticated caller.
	if p.Sources, err = s.loadSources(p.Name); err != nil {
		return nil, err
	}
	return &p, nil
}

// inTx runs fn inside one transaction and rolls back on any error, so a
// multi-statement write is all-or-nothing.
func (s *PGStore) inTx(what string, fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin %s: %w", what, err)
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", what, err)
	}
	return nil
}

// projectDefaults fills the fields the store supplies when a caller leaves
// them empty. Mode defaults to this store's instance-wide default (see
// design.md D2) - the same fallback the schema migration backfills existing
// rows with, so a project created without an explicit Mode behaves exactly
// like one that predates this field.
func (s *PGStore) projectDefaults(cfg *ProjectConfig) {
	if cfg.CreatedAt == "" {
		cfg.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if cfg.DefaultBranch == "" {
		cfg.DefaultBranch = "main"
	}
	if cfg.PipelineFile == "" {
		cfg.PipelineFile = "pipeline.tf"
	}
	if cfg.Mode == "" {
		cfg.Mode = s.defaultMode
	}
}

func upsertProjectTx(tx *sql.Tx, cfg *ProjectConfig) error {
	if _, err := tx.Exec(
		`INSERT INTO projects (name, repo_url, repo_ref, repo_credential_ref, pipeline_dir, pipeline_file, gitlab_project_id, default_branch, webhook_secret, mode, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 ON CONFLICT (name) DO UPDATE SET repo_url = EXCLUDED.repo_url, repo_ref = EXCLUDED.repo_ref, repo_credential_ref = EXCLUDED.repo_credential_ref, pipeline_dir = EXCLUDED.pipeline_dir, pipeline_file = EXCLUDED.pipeline_file, gitlab_project_id = EXCLUDED.gitlab_project_id, default_branch = EXCLUDED.default_branch, webhook_secret = EXCLUDED.webhook_secret, mode = EXCLUDED.mode`,
		cfg.Name, cfg.RepoURL, cfg.RepoRef, nullIfEmpty(cfg.RepoCredentialRef), cfg.PipelineDir, cfg.PipelineFile, cfg.GitLabProjectID, cfg.DefaultBranch, cfg.WebhookSecret, cfg.Mode, cfg.CreatedAt,
	); err != nil {
		return fmt.Errorf("upsert %s: %w", cfg.Name, err)
	}
	return nil
}

// writePrimarySourceTx keeps the primary source row in step with repo_url. A
// repo_url that yields no key (validateProject rejects those) simply leaves
// routing to the gitlab_project_id fallback.
func writePrimarySourceTx(tx *sql.Tx, cfg *ProjectConfig) error {
	key, err := source.SourceMatchKey(cfg.RepoURL)
	if err != nil {
		return nil
	}
	if _, err := tx.Exec(`DELETE FROM project_sources WHERE project_name = $1 AND is_primary AND match_key <> $2`, cfg.Name, key); err != nil {
		return fmt.Errorf("retire primary source of %s: %w", cfg.Name, err)
	}
	if _, err := tx.Exec(`INSERT INTO project_sources (project_name, match_key, source_name, repo_url, is_primary)
		VALUES ($1, $2, $1, $3, true)
		ON CONFLICT (project_name, match_key) DO UPDATE SET source_name = EXCLUDED.source_name, repo_url = EXCLUDED.repo_url, is_primary = true`,
		cfg.Name, key, cfg.RepoURL); err != nil {
		return fmt.Errorf("record primary source of %s: %w", cfg.Name, err)
	}
	return nil
}

func replaceSourcesTx(tx *sql.Tx, name string, sources []ProjectSource) error {
	if _, err := tx.Exec(`DELETE FROM project_sources WHERE project_name = $1`, name); err != nil {
		return fmt.Errorf("clear sources for %s: %w", name, err)
	}
	for _, src := range sources {
		key, err := source.SourceMatchKey(src.URL)
		if err != nil {
			return fmt.Errorf("source %s of %s: %w", src.Name, name, err)
		}
		if _, err := tx.Exec(`INSERT INTO project_sources (project_name, match_key, source_name, repo_url, is_primary)
			VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`, name, key, src.Name, src.URL, src.Primary); err != nil {
			return fmt.Errorf("insert source %s for %s: %w", src.Name, name, err)
		}
	}
	return nil
}

// Upsert writes the project row and, in the same transaction, keeps its primary
// source row in step with repo_url, so a project seeded without a derived set
// still routes. Secondary rows are owned by ReplaceSources.
func (s *PGStore) Upsert(cfg *ProjectConfig) error {
	s.projectDefaults(cfg)
	return s.inTx("upsert "+cfg.Name, func(tx *sql.Tx) error {
		if err := upsertProjectTx(tx, cfg); err != nil {
			return err
		}
		return writePrimarySourceTx(tx, cfg)
	})
}

// UpsertWithSources writes the project row and the project's complete source
// set in one transaction. Create and update use it so a rejected source set
// leaves the registration exactly as it was: an error response never leaves a
// new project, or a new repo_url, secret or ref, behind.
func (s *PGStore) UpsertWithSources(cfg *ProjectConfig, sources []ProjectSource) error {
	s.projectDefaults(cfg)
	return s.inTx("upsert "+cfg.Name+" with sources", func(tx *sql.Tx) error {
		if err := upsertProjectTx(tx, cfg); err != nil {
			return err
		}
		return replaceSourcesTx(tx, cfg.Name, sources)
	})
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *PGStore) Delete(name string) error {
	result, err := s.db.Exec(`DELETE FROM projects WHERE name = $1`, name)
	if err != nil {
		return fmt.Errorf("delete %s: %w", name, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("project %q not found", name)
	}
	return nil
}

func (s *PGStore) List() ([]string, error) {
	rows, err := s.db.Query(`SELECT name FROM projects ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan name: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// CreateRun inserts a new run and returns its ID.
