package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"kestrix/internal/credentials"
	"kestrix/internal/source"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PGStore provides PostgreSQL-backed project configuration storage.
type PGStore struct {
	db                  *sql.DB
	masterKeys          credentials.MasterKeyProvider
	webhookHistoryLimit int
	runHistoryLimit     int
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

	return &PGStore{db: db, webhookHistoryLimit: DefaultWebhookHistoryLimit, runHistoryLimit: DefaultRunHistoryLimit}, nil
}

// SetWebhookHistoryLimit bounds how many webhook events are retained. A
// non-positive limit restores the default.
func (s *PGStore) SetWebhookHistoryLimit(limit int) {
	if limit <= 0 {
		limit = DefaultWebhookHistoryLimit
	}
	s.webhookHistoryLimit = limit
}

// SetRunHistoryLimit bounds how many terminal runs are retained; see
// run-concurrency-and-retention design.md D3. A non-positive limit restores
// the default.
func (s *PGStore) SetRunHistoryLimit(limit int) {
	if limit <= 0 {
		limit = DefaultRunHistoryLimit
	}
	s.runHistoryLimit = limit
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

		CREATE TABLE IF NOT EXISTS webhook_events (
			id              SERIAL PRIMARY KEY,
			received_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
			project_name    TEXT,
			repo            TEXT NOT NULL DEFAULT '',
			branch          TEXT NOT NULL DEFAULT '',
			revision        TEXT NOT NULL DEFAULT '',
			commits         INTEGER NOT NULL DEFAULT 0,
			triggered_tasks JSONB NOT NULL DEFAULT '[]',
			status          TEXT NOT NULL,
			error           TEXT NOT NULL DEFAULT '',
			dispatch        JSONB NOT NULL DEFAULT '{}'
		);

		CREATE INDEX IF NOT EXISTS idx_webhook_events_received ON webhook_events(received_at DESC, id DESC);
		CREATE INDEX IF NOT EXISTS idx_webhook_events_project ON webhook_events(project_name);
		CREATE INDEX IF NOT EXISTS idx_webhook_events_status ON webhook_events(status);

		CREATE TABLE IF NOT EXISTS users (
			name           TEXT PRIMARY KEY,
			password_hash  TEXT NOT NULL,
			role           TEXT NOT NULL,
			created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
		);

		CREATE TABLE IF NOT EXISTS project_members (
			user_name    TEXT NOT NULL REFERENCES users(name) ON DELETE CASCADE,
			project_name TEXT NOT NULL REFERENCES projects(name) ON DELETE CASCADE,
			role         TEXT NOT NULL,
			PRIMARY KEY (user_name, project_name)
		);

		CREATE TABLE IF NOT EXISTS project_sources (
			project_name TEXT NOT NULL REFERENCES projects(name) ON DELETE CASCADE,
			match_key    TEXT NOT NULL,
			source_name  TEXT NOT NULL,
			repo_url     TEXT NOT NULL,
			is_primary   BOOLEAN NOT NULL DEFAULT false,
			PRIMARY KEY (project_name, match_key)
		);

		CREATE INDEX IF NOT EXISTS idx_project_sources_key ON project_sources(match_key);
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
		{"add user token version", `ALTER TABLE users ADD COLUMN IF NOT EXISTS token_version INTEGER NOT NULL DEFAULT 1`},
		{"add run started_by", `ALTER TABLE runs ADD COLUMN IF NOT EXISTS started_by TEXT NOT NULL DEFAULT ''`},
		{"add run canceled_by", `ALTER TABLE runs ADD COLUMN IF NOT EXISTS canceled_by TEXT NOT NULL DEFAULT ''`},
		{"add webhook event source", `ALTER TABLE webhook_events ADD COLUMN IF NOT EXISTS source_name TEXT NOT NULL DEFAULT ''`},
		// Supports pruneRuns' `WHERE status IN (...) ORDER BY started_at DESC, id
		// DESC LIMIT $1`, run inside every FinishRun/CancelRun transaction;
		// idx_runs_status alone (status only) leaves that ORDER BY an unindexed
		// sort over every terminal row - see REVIEW.md R4.
		{"add runs status/started_at index", `CREATE INDEX IF NOT EXISTS idx_runs_status_started ON runs(status, started_at DESC, id DESC)`},
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
	if err := backfillPrimarySources(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migration: %w", err)
	}
	return nil
}

// backfillPrimarySources gives every project that predates project_sources
// its primary row, so a push to its repository keeps resolving after the
// upgrade. Secondary rows need the parsed pipeline and appear on the first
// definition load; startup never touches Git. The match key is computed in
// Go, which is why this is a loop rather than one INSERT ... SELECT.
func backfillPrimarySources(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT name, repo_url FROM projects p
		WHERE NOT EXISTS (SELECT 1 FROM project_sources s WHERE s.project_name = p.name) ORDER BY name`)
	if err != nil {
		return fmt.Errorf("find projects without sources: %w", err)
	}
	type primary struct{ name, url string }
	var missing []primary
	for rows.Next() {
		var p primary
		if err := rows.Scan(&p.name, &p.url); err != nil {
			rows.Close()
			return fmt.Errorf("scan project without sources: %w", err)
		}
		missing = append(missing, p)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close projects without sources: %w", err)
	}
	for _, p := range missing {
		key, err := source.SourceMatchKey(p.url)
		if err != nil {
			// The row stays without sources and keeps resolving through its
			// gitlab_project_id; a malformed URL must not block startup.
			continue
		}
		if _, err := tx.Exec(`INSERT INTO project_sources (project_name, match_key, source_name, repo_url, is_primary)
			VALUES ($1, $2, $1, $3, true) ON CONFLICT DO NOTHING`, p.name, key, p.url); err != nil {
			return fmt.Errorf("backfill primary source for %s: %w", p.name, err)
		}
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
