## ADDED Requirements

### Requirement: PostgreSQL is mandatory for runtime persistence

Kestrix SHALL use PostgreSQL as the only runtime persistence backend for project configuration and pipeline runs. The server SHALL NOT fall back to a JSON or in-memory persistence store when PostgreSQL is unavailable.

#### Scenario: Server starts with a working database

- **WHEN** the configured PostgreSQL connection and required schema are available
- **THEN** the server SHALL initialize project and run stores backed by PostgreSQL
- **AND** the HTTP server SHALL start normally

#### Scenario: Server starts without a working database

- **WHEN** the configured PostgreSQL connection fails or the required schema is unavailable
- **THEN** startup SHALL return a descriptive error with credentials redacted
- **AND** the HTTP server SHALL not accept requests

### Requirement: Pipeline runs have durable identifiers

Every accepted pipeline run SHALL be persisted in PostgreSQL before background execution begins. The run endpoint SHALL return the database-generated positive run identifier.

#### Scenario: Run is created successfully

- **WHEN** a valid pipeline run request is received and PostgreSQL accepts the run record
- **THEN** the response SHALL include a positive `run_id`
- **AND** background execution SHALL use that persisted run record

#### Scenario: Run cannot be persisted

- **WHEN** PostgreSQL rejects run creation
- **THEN** the endpoint SHALL return an error response
- **AND** it SHALL not report the run as started
- **AND** no response with `run_id: 0` SHALL be considered successful

### Requirement: FileStore is removed from production runtime

The application SHALL not construct, select, or expose a JSON FileStore backend at runtime. Project CRUD, run history, cancellation, and task approval SHALL use PostgreSQL-backed stores.

#### Scenario: Database connection is lost during startup

- **WHEN** PostgreSQL cannot be initialized
- **THEN** the application SHALL fail startup
- **AND** it SHALL not switch to file persistence
