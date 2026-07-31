# docker-project-identity Specification

## Purpose
TBD - created by archiving change rename-docker-project-references. Update Purpose after archive.
## Requirements
### Requirement: Docker resources use the Kestrix identity

Docker development and production configurations SHALL use `kestrix` for project-facing application service names and container names. Compose dependency references SHALL resolve using the renamed service names. Existing PostgreSQL database, user, password, and test-database names SHALL remain unchanged for application and test compatibility.

#### Scenario: Development Compose configuration

- **WHEN** a developer starts the development Compose configuration
- **THEN** the application service and its project-facing container identity SHALL use `kestrix`
- **AND** PostgreSQL and Redis dependencies SHALL remain reachable through valid Compose service references
- **AND** the existing published application port SHALL remain unchanged

#### Scenario: Production Compose configuration

- **WHEN** an operator starts the production Compose configuration
- **THEN** project-facing application and supporting container identities SHALL use the Kestrix naming convention
- **AND** application, PostgreSQL, and Redis services SHALL retain their existing topology and published ports

### Requirement: Docker helper commands match service names

Docker Makefile targets and operational messages SHALL refer to the Kestrix service and SHALL not issue commands against a stale application service name.

#### Scenario: Follow application logs

- **WHEN** an operator runs the Docker Makefile target for Kestrix logs
- **THEN** the command SHALL select the renamed Kestrix application service
- **AND** the help text SHALL identify the service as Kestrix

#### Scenario: Open an application shell

- **WHEN** an operator runs the Docker Makefile shell target
- **THEN** the command SHALL execute against the renamed Kestrix application service

### Requirement: Preserve application configuration compatibility

The Docker rename SHALL preserve existing `METACI_*` environment-variable names, PostgreSQL connection defaults, and SHALL not change application ports, image defaults, or secret injection behavior.

#### Scenario: Existing environment configuration

- **WHEN** a deployment provides existing `METACI_*` environment variables
- **THEN** Docker Compose SHALL continue to pass those variables to the application
- **AND** no secret value SHALL be introduced into the repository as part of the rename

### Requirement: Existing resources are not deleted implicitly

The rename SHALL NOT automatically delete, migrate, or overwrite existing Docker containers, volumes, or database data.

#### Scenario: Existing installation upgrade

- **WHEN** an operator applies the renamed Docker configuration to an installation using old resource names
- **THEN** the configuration SHALL require explicit operator action to recreate or migrate resources
- **AND** the change SHALL not silently remove existing data

