package store

import (
	"database/sql"
	"fmt"
	"time"
)

func (s *PGStore) Load() (map[string]*ProjectConfig, error) {
	rows, err := s.db.Query(`SELECT name, repo_path, repo_url, pipeline_dir, pipeline_file, gitlab_project_id, default_branch, webhook_secret, created_at FROM projects`)
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()

	projects := make(map[string]*ProjectConfig)
	for rows.Next() {
		var p ProjectConfig
		var createdAt time.Time
		if err := rows.Scan(&p.Name, &p.RepoPath, &p.RepoURL, &p.PipelineDir, &p.PipelineFile, &p.GitLabProjectID, &p.DefaultBranch, &p.WebhookSecret, &createdAt); err != nil {
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
		if _, err := tx.Exec(
			`INSERT INTO projects (name, repo_path, repo_url, pipeline_dir, pipeline_file, gitlab_project_id, default_branch, webhook_secret, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			p.Name, p.RepoPath, p.RepoURL, p.PipelineDir, p.PipelineFile, p.GitLabProjectID, p.DefaultBranch, p.WebhookSecret, p.CreatedAt,
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
		`SELECT name, repo_path, repo_url, pipeline_dir, pipeline_file, gitlab_project_id, default_branch, webhook_secret, created_at FROM projects WHERE name = $1`, name,
	).Scan(&p.Name, &p.RepoPath, &p.RepoURL, &p.PipelineDir, &p.PipelineFile, &p.GitLabProjectID, &p.DefaultBranch, &p.WebhookSecret, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get project %s: %w", name, err)
	}
	p.CreatedAt = createdAt.Format(time.RFC3339)
	return &p, nil
}

func (s *PGStore) FindByGitLabID(id int) (*ProjectConfig, error) {
	var p ProjectConfig
	var createdAt time.Time
	err := s.db.QueryRow(
		`SELECT name, repo_path, repo_url, pipeline_dir, pipeline_file, gitlab_project_id, default_branch, webhook_secret, created_at FROM projects WHERE gitlab_project_id = $1`, id,
	).Scan(&p.Name, &p.RepoPath, &p.RepoURL, &p.PipelineDir, &p.PipelineFile, &p.GitLabProjectID, &p.DefaultBranch, &p.WebhookSecret, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find by gitlab id %d: %w", id, err)
	}
	p.CreatedAt = createdAt.Format(time.RFC3339)
	return &p, nil
}

func (s *PGStore) Upsert(cfg *ProjectConfig) error {
	if cfg.CreatedAt == "" {
		cfg.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if cfg.DefaultBranch == "" {
		cfg.DefaultBranch = "main"
	}
	if cfg.PipelineFile == "" {
		cfg.PipelineFile = "pipeline.tf"
	}
	if _, err := s.db.Exec(
		`INSERT INTO projects (name, repo_path, repo_url, pipeline_dir, pipeline_file, gitlab_project_id, default_branch, webhook_secret, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (name) DO UPDATE SET repo_path = EXCLUDED.repo_path, repo_url = EXCLUDED.repo_url, pipeline_dir = EXCLUDED.pipeline_dir, pipeline_file = EXCLUDED.pipeline_file, gitlab_project_id = EXCLUDED.gitlab_project_id, default_branch = EXCLUDED.default_branch, webhook_secret = EXCLUDED.webhook_secret`,
		cfg.Name, cfg.RepoPath, cfg.RepoURL, cfg.PipelineDir, cfg.PipelineFile, cfg.GitLabProjectID, cfg.DefaultBranch, cfg.WebhookSecret, cfg.CreatedAt,
	); err != nil {
		return fmt.Errorf("upsert %s: %w", cfg.Name, err)
	}
	return nil
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
