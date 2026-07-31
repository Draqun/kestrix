## Why

Kestrix assumes PostgreSQL for run persistence, but the server still silently falls back to FileStore when the database is unavailable. FileStore cannot create or query runs, so a local pipeline execution can appear to start while returning `run_id: 0` and leaving the UI without a trackable run. PostgreSQL must be an explicit startup requirement rather than an optional dependency.

## What Changes

- **BREAKING** Remove FileStore and its JSON project persistence implementation from the application.
- **BREAKING** Require a successful PostgreSQL connection and schema initialization before the HTTP server starts.
- Remove the silent database-to-file fallback and return a clear startup error when PostgreSQL is unavailable.
- Make project registration, pipeline execution, run history, cancellation, and task approval use PostgreSQL-backed stores exclusively.
- Remove FileStore-specific branches, tests, documentation, and fallback configuration.
- Ensure run endpoints never report a successful start without a valid persisted `run_id`.

## Capabilities

### New Capabilities

- `database-backed-runtime`: PostgreSQL is the mandatory persistence layer for projects and pipeline runs.

### Modified Capabilities

- `project-registration`: Project configuration is persisted only in PostgreSQL.

## Impact

- Affected packages: `internal/store`, `internal/web`, `internal/service`, CLI startup, and tests.
- The server startup contract and local Docker setup.
- Existing users with JSON FileStore data need a one-time migration before upgrading.
- PostgreSQL credentials, database naming, and schema initialization become mandatory operational prerequisites.

## Non-goals

- Replacing PostgreSQL with another database.
- Designing a general data migration framework.
- Changing pipeline DAG semantics or runner behavior.
