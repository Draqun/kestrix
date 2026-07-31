## 1. Make PostgreSQL mandatory

- [x] 1.1 Refactor CLI/server startup and store initialization to return an error when PostgreSQL is missing, unreachable, or has an invalid schema.
- [x] 1.2 Remove the database-to-FileStore fallback and ensure startup errors redact credentials and connection secrets.
- [x] 1.3 Add startup tests covering successful PostgreSQL wiring and failure without an HTTP listener.

## 2. Remove FileStore runtime support

- [x] 2.1 Delete the FileStore implementation and remove all production references to JSON project persistence.
- [x] 2.2 Remove FileStore-specific branches from project, run, cancellation, and task-approval handlers.
- [x] 2.3 Replace FileStore tests with PostgreSQL integration coverage or injected mock-store tests where appropriate.
- [x] 2.4 Remove obsolete FileStore paths, fallback filenames, configuration, and documentation.

## 3. Guarantee valid run creation

- [x] 3.1 Make `RunService` reject a missing run store instead of returning `(0, nil)`.
- [x] 3.2 Add API/service tests proving successful runs return a positive persisted `run_id` and failures return an error.
- [x] 3.3 Verify run history, cancellation, and manual-task approval operate only against PostgreSQL records.

## 4. Data migration and local operations

- [x] 4.1 Define and document the one-time migration path from existing JSON project data to PostgreSQL.
- [x] 4.2 Update Docker, `.env.template`, CI, AGENTS.md, and operational docs to state PostgreSQL is required.
- [x] 4.3 Verify local `ci/` pipeline execution with Docker PostgreSQL and confirm the UI can open the returned run.

## 5. Verification

- [x] 5.1 Run `make fmt`, `make test`, `make build`, and the strict OpenSpec validation.
- [x] 5.2 Run a repository search confirming no production FileStore fallback or JSON persistence path remains.
