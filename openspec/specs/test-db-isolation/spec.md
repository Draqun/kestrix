# test-db-isolation Specification

## Purpose
TBD - created by archiving change fix-tests-use-separate-database. Update Purpose after archive.
## Requirements
### Requirement: Tests use a separate database env var
PGStore integration tests SHALL read `METACI_TEST_DATABASE_URL` for the database connection string, NOT `DATABASE_URL`. When `METACI_TEST_DATABASE_URL` is unset, the test helper SHALL default to `postgres://metaci:metaci@localhost:5432/metaci_test?sslmode=disable&connect_timeout=2`.

#### Scenario: Tests connect via METACI_TEST_DATABASE_URL
- **WHEN** `METACI_TEST_DATABASE_URL` is set to `postgres://user:pass@host:5432/metaci_test?sslmode=disable`
- **THEN** the test helper SHALL use that URL to connect
- **AND** the test helper SHALL NOT read `DATABASE_URL`

#### Scenario: METACI_TEST_DATABASE_URL not set
- **WHEN** `METACI_TEST_DATABASE_URL` is not set
- **THEN** the test helper SHALL default to `postgres://metaci:metaci@localhost:5432/metaci_test?sslmode=disable&connect_timeout=2`
- **AND** the database name in the default URL SHALL be `metaci_test`

### Requirement: Safety guard prevents connecting to dev database
The test helper SHALL refuse to connect to any database whose name does not end with `_test`. If the database name ends with `metaci` (without `_test`), the test SHALL fail immediately with a clear error message.

#### Scenario: Guard blocks connection to dev database
- **WHEN** the test database URL contains `/metaci?` (not `/metaci_test?`)
- **THEN** the test helper SHALL call `t.Fatal()` with a message explaining that tests must use a `_test`-suffixed database
- **AND** the test SHALL NOT connect to the database

#### Scenario: Guard allows connection to test database
- **WHEN** the test database URL contains `/metaci_test?`
- **THEN** the test helper SHALL proceed with the connection

### Requirement: Docker creates metaci_test database on init
The development `docker-compose.yml` SHALL create the `metaci_test` database automatically when PostgreSQL initializes for the first time.

#### Scenario: First docker-up creates test database
- **WHEN** `make docker-up` runs and the PostgreSQL volume is empty
- **THEN** the `metaci_test` database SHALL be created alongside `metaci`
- **AND** the test helper SHALL be able to connect to `metaci_test` without manual setup

#### Scenario: Subsequent docker-up preserves test database
- **WHEN** `make docker-up` runs with an existing PostgreSQL volume
- **THEN** the `metaci_test` database SHALL already exist
- **AND** no re-creation is needed

