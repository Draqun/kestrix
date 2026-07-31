## ADDED Requirements

### Requirement: Local and deployment databases are isolated

Kestrix runtime profiles SHALL use distinct PostgreSQL database identities and Docker storage resources for local development and deployment.

#### Scenario: Host-run local application uses the local database
- **WHEN** the local application starts without an explicit `DATABASE_URL`
- **THEN** it SHALL use the local development database URL with database name `kestrix_local` and the local host PostgreSQL port (default `5433`)

#### Scenario: Local Docker application uses the local database
- **WHEN** the development compose stack starts
- **THEN** the Kestrix service SHALL connect to `postgres:5432/kestrix_local`
- **AND** the PostgreSQL service SHALL use a local-only volume distinct from the deployment volume

#### Scenario: Deployed application uses the deployment database
- **WHEN** the production compose stack starts
- **THEN** the Kestrix service SHALL connect to the explicitly configured deployment URL for database `kestrix`
- **AND** it SHALL not use the local fallback URL

#### Scenario: Local and deployment data cannot overlap by default
- **WHEN** both runtime profiles are configured on the same host
- **THEN** they SHALL use different host ports, database names, and PostgreSQL volume identities
- **AND** a project created in one profile SHALL not appear in the other profile

#### Scenario: Configuration remains overrideable
- **WHEN** an operator supplies `DATABASE_URL` explicitly
- **THEN** the application SHALL use that URL instead of the local fallback
- **AND** no database credentials SHALL be hardcoded outside documented development defaults

#### Scenario: Test database remains protected
- **WHEN** the store integration tests run
- **THEN** they SHALL continue using `KESTRIX_TEST_DATABASE_URL`
- **AND** the test helper SHALL reject a database name that does not end in `_test`
