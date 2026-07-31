## MODIFIED Requirements

### Requirement: REQ-2: Project persistence (implemented)

Project configurations SHALL survive server restarts by being stored in PostgreSQL. The server SHALL not load or write project configurations through a JSON FileStore.

#### Scenario: Persistence across restarts

- **WHEN** the server starts with a working PostgreSQL database
- **THEN** it SHALL load registered projects from PostgreSQL

#### Scenario: Project mutation

- **WHEN** a project is created, updated, or deleted
- **THEN** the change SHALL be persisted in PostgreSQL
- **AND** subsequent API requests SHALL observe the change

#### Scenario: PostgreSQL is unavailable

- **WHEN** the server cannot connect to PostgreSQL
- **THEN** it SHALL fail startup
- **AND** it SHALL not create or read a JSON project store
