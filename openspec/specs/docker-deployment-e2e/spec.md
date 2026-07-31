# docker-deployment-e2e Specification

## Purpose
TBD - created by archiving change docker-deployment-e2e-tests. Update Purpose after archive.
## Requirements
### Requirement: Isolated Docker deployment E2E test

The repository SHALL provide a repeatable end-to-end test that builds or selects a Kestrix deployment image and starts the production Docker Compose stack under a unique test project, without reusing development or production containers, networks, databases, or volumes.

#### Scenario: Test starts an isolated deployment

- **WHEN** the Docker deployment E2E test is run with a working Docker daemon
- **THEN** it starts the Kestrix deployment stack with a unique Compose project, test-specific ports, and disposable database storage
- **AND** it does not stop or remove services belonging to the dev or normal deployment profiles

#### Scenario: Test fails clearly without Docker

- **WHEN** the Docker daemon or required Docker Compose command is unavailable
- **THEN** the test exits non-zero with a prerequisite-specific error

### Requirement: Deployment readiness verification

The test SHALL wait for the deployed Kestrix backend to become ready using a bounded retry loop and SHALL fail if readiness is not achieved within the configured timeout.

#### Scenario: Healthy deployment becomes ready

- **WHEN** the deployed backend responds successfully at its health endpoint
- **THEN** the readiness phase completes and the test proceeds to API assertions

#### Scenario: Deployment never becomes ready

- **WHEN** the health endpoint does not become successful before the timeout
- **THEN** the test exits non-zero and prints service status and relevant container logs

### Requirement: Deployment API smoke checks

The test SHALL verify the deployed HTTP API from outside the containers, including health and at least one database-backed project/run API response, without requiring preexisting persistent data.

#### Scenario: API responds from the deployed stack

- **WHEN** readiness succeeds
- **THEN** the test receives the expected HTTP status and JSON shape from the health endpoint and a database-backed API endpoint

#### Scenario: API or database is unavailable

- **WHEN** an API smoke check returns an unexpected status or malformed response
- **THEN** the test exits non-zero and includes the failing URL, status, and deployment logs

### Requirement: Deterministic cleanup and diagnostics

The test SHALL collect diagnostics on failure and SHALL remove only its dedicated Compose resources, including containers, networks, and disposable volumes, on both success and failure.

#### Scenario: Successful test cleans up

- **WHEN** all E2E assertions pass
- **THEN** the test exits zero and removes its temporary deployment resources

#### Scenario: Interrupted or failed test cleans up

- **WHEN** an assertion fails or the test receives an interrupt
- **THEN** it prints relevant logs and removes only resources associated with its unique test project

