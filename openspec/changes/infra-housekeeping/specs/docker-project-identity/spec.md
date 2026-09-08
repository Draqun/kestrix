## MODIFIED Requirements

### Requirement: Docker resources use the Kestrix identity

Docker development and production configurations SHALL use `kestrix` for project-facing application service names and container names. Compose dependency references SHALL resolve using the renamed service names. Existing PostgreSQL database, user, password, and test-database names SHALL remain unchanged for application and test compatibility. The Compose configurations SHALL declare only the services the application connects to; a supporting service that no application code uses SHALL NOT be declared.

#### Scenario: Development Compose configuration

- **WHEN** a developer starts the development Compose configuration
- **THEN** the application service and its project-facing container identity SHALL use `kestrix`
- **AND** the PostgreSQL dependency SHALL remain reachable through a valid Compose service reference
- **AND** the existing published application port SHALL remain unchanged
- **AND** no Redis service or volume SHALL be declared

#### Scenario: Production Compose configuration

- **WHEN** an operator starts the production Compose configuration
- **THEN** project-facing application and supporting container identities SHALL use the Kestrix naming convention
- **AND** the application and PostgreSQL services SHALL retain their existing topology and published ports
- **AND** no Redis service or volume SHALL be declared, so the stack publishes no port the application does not use

#### Scenario: Existing Redis volume is not removed implicitly

- **WHEN** an operator applies the configuration to an installation that still holds a `redis_data` volume from an earlier release
- **THEN** Compose SHALL leave that volume in place
- **AND** the operator removes it with an explicit command
