package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"kestrix/internal/credentials"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PGStore provides PostgreSQL-backed project configuration storage.
type PGStore struct {
	db         *sql.DB
	masterKeys credentials.MasterKeyProvider
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

	return &PGStore{db: db, masterKeys: credentials.OSKeyringProvider{}}, nil
}

func migrate(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS credentials (
			credential_ref TEXT PRIMARY KEY,
			name           TEXT NOT NULL,
			credential_type TEXT NOT NULL,
			provider       TEXT NOT NULL DEFAULT '',
			host           TEXT NOT NULL DEFAULT '',
			encrypted_payload BYTEA NOT NULL,
			key_version    INTEGER NOT NULL,
			revoked        BOOLEAN NOT NULL DEFAULT false,
			created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
		);

		CREATE TABLE IF NOT EXISTS projects (
			name            TEXT PRIMARY KEY,
			repo_url        TEXT NOT NULL,
			repo_ref        TEXT NOT NULL,
			repo_credential_ref TEXT,
			pipeline_dir    TEXT NOT NULL DEFAULT 'ci',
			pipeline_file   TEXT NOT NULL DEFAULT 'pipeline.tf',
			gitlab_project_id INTEGER NOT NULL DEFAULT 0,
			default_branch  TEXT NOT NULL DEFAULT 'main',
			webhook_secret  TEXT NOT NULL DEFAULT '',
			created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT projects_repo_url_nonempty CHECK (btrim(repo_url) <> ''),
			CONSTRAINT projects_repo_ref_nonempty CHECK (btrim(repo_ref) <> '')
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
			sources         JSONB NOT NULL DEFAULT '[]',
			started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			finished_at     TIMESTAMPTZ
		);

		CREATE INDEX IF NOT EXISTS idx_runs_project ON runs(project_name);
		CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);
	`); err != nil {
		return fmt.Errorf("create base schema: %w", err)
	}

	statements := []struct {
		name string
		sql  string
	}{
		{"add run task results", `ALTER TABLE runs ADD COLUMN IF NOT EXISTS task_results JSONB NOT NULL DEFAULT '[]'`},
		{"add run pipeline snapshot", `ALTER TABLE runs ADD COLUMN IF NOT EXISTS pipeline_def JSONB`},
		{"add run sources", `ALTER TABLE runs ADD COLUMN IF NOT EXISTS sources JSONB NOT NULL DEFAULT '[]'`},
		{"add project URL", `ALTER TABLE projects ADD COLUMN IF NOT EXISTS repo_url TEXT NOT NULL DEFAULT ''`},
		{"add project ref", `ALTER TABLE projects ADD COLUMN IF NOT EXISTS repo_ref TEXT NOT NULL DEFAULT ''`},
		{"add project credential reference", `ALTER TABLE projects ADD COLUMN IF NOT EXISTS repo_credential_ref TEXT`},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement.sql); err != nil {
			return fmt.Errorf("%s: %w", statement.name, err)
		}
	}

	rows, err := tx.Query(`SELECT name FROM projects WHERE btrim(repo_url) = '' OR btrim(repo_ref) = '' ORDER BY name`)
	if err != nil {
		return fmt.Errorf("preflight remote projects: %w", err)
	}
	var legacy []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return fmt.Errorf("scan legacy project: %w", err)
		}
		legacy = append(legacy, name)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close legacy project rows: %w", err)
	}
	if len(legacy) > 0 {
		return fmt.Errorf("local-only projects require an explicit remote URL/ref update or deletion before upgrade: %v", legacy)
	}

	remoteStatements := []struct {
		name string
		sql  string
	}{
		{"require project URL", `ALTER TABLE projects ALTER COLUMN repo_url SET NOT NULL`},
		{"remove project URL default", `ALTER TABLE projects ALTER COLUMN repo_url DROP DEFAULT`},
		{"require project ref", `ALTER TABLE projects ALTER COLUMN repo_ref SET NOT NULL`},
		{"remove project ref default", `ALTER TABLE projects ALTER COLUMN repo_ref DROP DEFAULT`},
		{"make credential reference nullable", `ALTER TABLE projects ALTER COLUMN repo_credential_ref DROP NOT NULL`},
		{"remove credential reference default", `ALTER TABLE projects ALTER COLUMN repo_credential_ref DROP DEFAULT`},
		{"drop local project path", `ALTER TABLE projects DROP COLUMN IF EXISTS repo_path`},
		{"add remote source checks", `DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'projects_repo_url_nonempty' AND conrelid = 'projects'::regclass)
			THEN ALTER TABLE projects ADD CONSTRAINT projects_repo_url_nonempty CHECK (btrim(repo_url) <> ''); END IF;
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'projects_repo_ref_nonempty' AND conrelid = 'projects'::regclass)
			THEN ALTER TABLE projects ADD CONSTRAINT projects_repo_ref_nonempty CHECK (btrim(repo_ref) <> ''); END IF;
		END $$`},
		{"add project credential foreign key", `DO $$ BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'projects_repo_credential_ref_fk' AND conrelid = 'projects'::regclass)
		THEN
			IF EXISTS (SELECT 1 FROM projects p LEFT JOIN credentials c ON p.repo_credential_ref = c.credential_ref WHERE p.repo_credential_ref IS NOT NULL AND c.credential_ref IS NULL)
			THEN RAISE EXCEPTION 'project contains an unknown credential reference'; END IF;
			ALTER TABLE projects ADD CONSTRAINT projects_repo_credential_ref_fk FOREIGN KEY (repo_credential_ref) REFERENCES credentials(credential_ref);
		END IF;
	END $$`},
		{"index credential host", `CREATE INDEX IF NOT EXISTS idx_credentials_host ON credentials(host)`},
		{"index credential revoked", `CREATE INDEX IF NOT EXISTS idx_credentials_revoked ON credentials(revoked)`},
	}
	for _, statement := range remoteStatements {
		if _, err := tx.Exec(statement.sql); err != nil {
			return fmt.Errorf("%s: %w", statement.name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migration: %w", err)
	}
	return nil
}

// SetCredentialMasterKeyProvider configures key custody for credential
// operations. It is primarily used by deployment wiring and integration tests.
func (s *PGStore) SetCredentialMasterKeyProvider(provider credentials.MasterKeyProvider) {
	if provider != nil {
		s.masterKeys = provider
	}
}
