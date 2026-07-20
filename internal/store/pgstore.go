package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PGStore provides PostgreSQL-backed project configuration storage.
type PGStore struct {
	db *sql.DB
}

// NewPG creates a PGStore, connects to the database, and ensures the schema exists.
func NewPG(databaseURL string) (*PGStore, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &PGStore{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS projects (
			name            TEXT PRIMARY KEY,
			repo_path       TEXT NOT NULL DEFAULT '',
			repo_url        TEXT NOT NULL DEFAULT '',
			pipeline_dir    TEXT NOT NULL DEFAULT '',
			pipeline_file   TEXT NOT NULL DEFAULT 'pipeline.tf',
			gitlab_project_id INTEGER NOT NULL DEFAULT 0,
			default_branch  TEXT NOT NULL DEFAULT 'main',
			webhook_secret  TEXT NOT NULL DEFAULT '',
			created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
		);

		CREATE TABLE IF NOT EXISTS runs (
			id              SERIAL PRIMARY KEY,
			project_name    TEXT NOT NULL REFERENCES projects(name) ON DELETE CASCADE,
			status          TEXT NOT NULL DEFAULT 'running',
			changed         JSONB NOT NULL DEFAULT '[]',
			include_manual  BOOLEAN NOT NULL DEFAULT false,
			output          TEXT NOT NULL DEFAULT '',
			error           TEXT NOT NULL DEFAULT '',
			task_results    JSONB NOT NULL DEFAULT '[]',
			started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			finished_at     TIMESTAMPTZ
		);

		CREATE INDEX IF NOT EXISTS idx_runs_project ON runs(project_name);
		CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);
	`)
	if err != nil {
		return err
	}
	// Migration: add task_results column if missing (for existing databases).
	_, _ = db.Exec(`ALTER TABLE runs ADD COLUMN IF NOT EXISTS task_results JSONB NOT NULL DEFAULT '[]'`)
	return nil
}

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

// Run represents a single pipeline execution.
type Run struct {
	ID            int              `json:"id"`
	ProjectName   string           `json:"project_name"`
	Status        string           `json:"status"` // running, passed, failed
	Changed       []string         `json:"changed"`
	IncludeManual bool             `json:"include_manual"`
	Output        string           `json:"output"`
	Error         string           `json:"error,omitempty"`
	TaskResults   []TaskResult     `json:"task_results,omitempty"`
	StartedAt     time.Time        `json:"started_at"`
	FinishedAt    *time.Time       `json:"finished_at,omitempty"`
}

// TaskResult is a per-task execution record stored in the runs table.
type TaskResult struct {
	Name     string        `json:"name"`
	Status   string        `json:"status"`
	Output   string        `json:"output"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration"`
}

// CreateRun inserts a new run and returns its ID.
func (s *PGStore) CreateRun(projectName string, changed []string, includeManual bool) (int, error) {
	changedJSON := "[]"
	if len(changed) > 0 {
		b, _ := json.Marshal(changed)
		changedJSON = string(b)
	}
	var id int
	err := s.db.QueryRow(
		`INSERT INTO runs (project_name, changed, include_manual) VALUES ($1, $2, $3) RETURNING id`,
		projectName, changedJSON, includeManual,
	).Scan(&id)
	return id, err
}

// FinishRun updates a run with its result.
func (s *PGStore) FinishRun(id int, status, output, errMsg string, taskResults []TaskResult) error {
	taskResultsJSON := "[]"
	if len(taskResults) > 0 {
		b, _ := json.Marshal(taskResults)
		taskResultsJSON = string(b)
	}
	_, err := s.db.Exec(
		`UPDATE runs SET status = $1, output = $2, error = $3, task_results = $4, finished_at = now() WHERE id = $5`,
		status, output, errMsg, taskResultsJSON, id,
	)
	return err
}

// GetRun returns a run by ID.
func (s *PGStore) GetRun(id int) (*Run, error) {
	var r Run
	var changedJSON, taskResultsJSON string
	var finishedAt sql.NullTime
	err := s.db.QueryRow(
		`SELECT id, project_name, status, changed, include_manual, output, error, task_results, started_at, finished_at FROM runs WHERE id = $1`, id,
	).Scan(&r.ID, &r.ProjectName, &r.Status, &changedJSON, &r.IncludeManual, &r.Output, &r.Error, &taskResultsJSON, &r.StartedAt, &finishedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(changedJSON), &r.Changed)
	json.Unmarshal([]byte(taskResultsJSON), &r.TaskResults)
	if finishedAt.Valid {
		r.FinishedAt = &finishedAt.Time
	}
	return &r, nil
}

// ListRuns returns runs for a project, most recent first.
func (s *PGStore) ListRuns(projectName string, limit int) ([]Run, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT id, project_name, status, changed, include_manual, output, error, task_results, started_at, finished_at FROM runs WHERE project_name = $1 ORDER BY started_at DESC LIMIT $2`,
		projectName, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		var r Run
		var changedJSON, taskResultsJSON string
		var finishedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.ProjectName, &r.Status, &changedJSON, &r.IncludeManual, &r.Output, &r.Error, &taskResultsJSON, &r.StartedAt, &finishedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(changedJSON), &r.Changed)
		json.Unmarshal([]byte(taskResultsJSON), &r.TaskResults)
		if finishedAt.Valid {
			r.FinishedAt = &finishedAt.Time
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// ListAllRuns returns all runs across projects, most recent first.
func (s *PGStore) ListAllRuns(limit int) ([]Run, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT id, project_name, status, changed, include_manual, output, error, task_results, started_at, finished_at FROM runs ORDER BY started_at DESC LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		var r Run
		var changedJSON, taskResultsJSON string
		var finishedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.ProjectName, &r.Status, &changedJSON, &r.IncludeManual, &r.Output, &r.Error, &taskResultsJSON, &r.StartedAt, &finishedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(changedJSON), &r.Changed)
		json.Unmarshal([]byte(taskResultsJSON), &r.TaskResults)
		if finishedAt.Valid {
			r.FinishedAt = &finishedAt.Time
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}
