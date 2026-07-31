## ADDED Requirements

### Requirement: Pipeline with manual jobs shows as passed
The system SHALL mark a run as `"passed"` when all triggered tasks complete successfully, even if manual (untriggered) tasks exist.

#### Scenario: All tasks passed with manual jobs
- **WHEN** all triggered tasks finish with `"passed"` status
- **AND** one or more tasks have `"manual"` status (never triggered)
- **THEN** the run status is `"passed"`

#### Scenario: Manual task not reflected in dashboard stats
- **WHEN** querying run stats via `CountRunsByStatus`
- **THEN** no run has status `"manual"`
