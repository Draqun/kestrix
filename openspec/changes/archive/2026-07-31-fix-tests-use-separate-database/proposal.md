## Why

PGStore integration tests (`pgstore_integration_test.go`) connect to the same `DATABASE_URL` as the development server, then run `DELETE FROM runs; DELETE FROM projects;` on cleanup. Every `go test ./...` wipes the developer's manual test data, pipeline runs, and project configs. This makes iterative development painful — developers avoid running tests to preserve their data, which reduces test coverage in practice.

## What Changes

- Introduce `METACI_TEST_DATABASE_URL` env var for test database connections (separate from `DATABASE_URL`)
- Default test DB URL to `postgres://metaci:metaci@localhost:5432/metaci_test?sslmode=disable` (separate `metaci_test` database)
- Add a **safety guard** in `testPG()` that refuses to connect if the URL points at a database named `metaci` (not `metaci_test`)
- Add an init script to `docker/docker-compose.yml` postgres service that creates the `metaci_test` database automatically
- Update Makefile `test` target to not set `DATABASE_URL` (prevents tests from accidentally using dev DB)
- Update `.env.template` to document `METACI_TEST_DATABASE_URL`

## Capabilities

### New Capabilities
- None (this is a test isolation fix)

### Modified Capabilities
- None (no spec-level behavior changes)

## Impact

- **Test file**: `internal/store/pgstore_integration_test.go` — `testPG()` helper
- **Docker**: `docker/docker-compose.yml` — postgres init script for `metaci_test` database
- **Makefile**: `test` target — ensure `DATABASE_URL` is not inherited by tests
- **Config**: `.env.template` — document `METACI_TEST_DATABASE_URL`

## Non-goals

- Using testcontainers or ephemeral databases (too heavy for MVP)
- Transaction-based test isolation (tests already share state within a test DB)
- Changing the PGStore schema or migration system