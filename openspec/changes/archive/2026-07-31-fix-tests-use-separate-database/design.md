## Context

The Kestrix project uses PostgreSQL for project config and run history. The dev server connects via `DATABASE_URL` (default: `postgres://metaci:metaci@localhost:5432/metaci?sslmode=disable`). PGStore integration tests share this same database — `testPG()` reads `DATABASE_URL` and on cleanup executes `DELETE FROM runs; DELETE FROM projects;`.

This means every `go test ./...` destroys the developer's manual test data.

## Goals / Non-Goals

**Goals:**
- Tests MUST connect to a separate database (`metaci_test`) by default
- Tests MUST NOT read `DATABASE_URL` — only `METACI_TEST_DATABASE_URL`
- A safety guard MUST refuse to connect if the DB name is `metaci` (not `metaci_test`)
- `make docker-up` MUST automatically create the `metaci_test` database

**Non-Goals:**
- Ephemeral per-test databases (overkill for MVP)
- Transaction rollback isolation (tests within a suite can share state)
- CI-specific database provisioning (CI uses its own `METACI_TEST_DATABASE_URL`)

## Decisions

### Decision 1: Separate env var `METACI_TEST_DATABASE_URL`

**Choice**: Tests read `METACI_TEST_DATABASE_URL` instead of `DATABASE_URL`.

**Rationale**: `DATABASE_URL` is the production/dev connection string. Using a separate env var makes it impossible for tests to accidentally wipe the dev database.

**Default value**: `postgres://metaci:metaci@localhost:5432/metaci_test?sslmode=disable&connect_timeout=2`

### Decision 2: Safety guard in testPG()

**Choice**: Parse the test DB URL and refuse to connect if the database name is `metaci` (i.e. pointing at the dev database). Only allow databases ending in `_test`.

**Rationale**: Even with separate env vars, a developer could accidentally set `METACI_TEST_DATABASE_URL` to the dev database. A guard prevents data loss.

**Implementation**: Extract the database name from the URL using `net/url` parsing. If it doesn't end with `_test`, `t.Fatal()` with a clear message.

### Decision 3: Docker init script for metaci_test

**Choice**: Add a `docker/postgres-init.sh` script mounted into `/docker-entrypoint-initdb.d/` that creates the `metaci_test` database.

**Rationale**: PostgreSQL's official image runs scripts in `/docker-entrypoint-initdb.d/` on first init. This automatically creates the test database when `make docker-up` runs for the first time.

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| Existing volume already initialized — init script won't run | Document `docker compose down -v` to recreate volumes |
| CI doesn't have `metaci_test` database | CI sets `METACI_TEST_DATABASE_URL` to its own test DB |
| URL parsing doesn't cover all PostgreSQL URL formats | Use `url.Parse` + extract path, handle both URL and DSN formats |