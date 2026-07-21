# Delta Spec: dag-engine

## MODIFIED Requirements

### Requirement: REQ-1: Documentation inclusion

The DAG engine documentation SHALL be included in the project-docs site and SHALL be kept up-to-date with any behaviour changes.

#### Scenario: Documentation included in UI build

- **WHEN** the documentation build runs,
- **THEN** the DAG engine documentation SHALL be present in `ui/dist/docs`.

#### Scenario: Documentation page is reachable from overview

- **WHEN** a user opens the project-docs overview page,
- **THEN** a link to the DAG engine documentation page SHALL be available.
