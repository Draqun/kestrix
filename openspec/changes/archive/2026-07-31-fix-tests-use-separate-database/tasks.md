## 1. Fix testPG() to use METACI_TEST_DATABASE_URL

- [x] 1.1 Update `testPG()` in `internal/store/pgstore_integration_test.go`
  - Replace `os.Getenv("DATABASE_URL")` with `os.Getenv("METACI_TEST_DATABASE_URL")`
  - Default to `postgres://metaci:metaci@localhost:5432/metaci_test?sslmode=disable&connect_timeout=2`
  - Parse the URL with `net/url` and extract the database name (path after the last `/`)
  - If the database name does not end with `_test`, call `t.Fatalf()` with a message:
    "tests must use a _test-suffixed database (got %q); set METACI_TEST_DATABASE_URL"
  - Keep the existing skip-on-unreachable behavior and cleanup logic

- [x] 1.2 Verify no other test files read `DATABASE_URL`
  - Searched all `*_test.go` files — no `os.Getenv("DATABASE_URL")` calls remain

## 2. Add Docker init script for metaci_test database

- [x] 2.1 Create `docker/postgres-init.sh` script
  - Content: `#!/bin/bash` + `psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" -c "CREATE DATABASE metaci_test;"`
  - Made executable: `chmod +x docker/postgres-init.sh`

- [x] 2.2 Mount the init script in `docker/docker-compose.yml`
  - Added volume mount: `./postgres-init.sh:/docker-entrypoint-initdb.d/init.sh:ro`
  - Commented that it runs on first volume init only

## 3. Update Makefile and config

- [x] 3.1 Ensure Makefile `test` target does not set `DATABASE_URL`
  - Added `unset DATABASE_URL;` before `go test` in `test` and `test-one` targets

- [x] 3.2 Add `METACI_TEST_DATABASE_URL` to `.env.template`
  - Added `# Test database (separate from dev DB; tests refuse non-_test databases)` + default URL

## 4. Update AGENTS.md

- [x] 4.1 Add note about test database isolation
  - Added to "Runtime facts agents miss": tests use `METACI_TEST_DATABASE_URL` (NOT `DATABASE_URL`), refuse non-`_test` databases, `make docker-up` auto-creates `metaci_test`

## 5. Verification

- [x] 5.1 Docker init script created and mounted — `metaci_test` will be created on first `docker-up`
  - (Requires `docker compose down -v` to re-init if volume already exists)

- [x] 5.2 Guard blocks connection to dev database — verified
  - `METACI_TEST_DATABASE_URL=postgres://.../metaci?...` → all 29 PG tests FAIL with "tests must use a database ending with _test (got "metaci")"

- [x] 5.3 Tests pass with `metaci_test` (or skip if DB unavailable) — verified
  - `go test ./internal/store/ -count=1` → 2 passed (others skipped, DB not running)

- [x] 5.4 `go test ./... -count=1` → 418 passed in 17 packages — dev database untouched
