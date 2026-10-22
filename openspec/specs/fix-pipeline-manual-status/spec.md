# fix-pipeline-manual-status Specification

## Purpose
TBD - created by archiving change fix-pipeline-manual-status. Update Purpose after archive.
## Requirements
### Requirement: A run with a task awaiting approval is running
The system SHALL keep a run `"running"` while any of its tasks is `"manual"` (awaiting approval), `"pending"` or `"running"`, and SHALL give it a terminal status only when no task is in one of those states.

#### Scenario: All tasks passed with manual jobs
- **WHEN** all other tasks finish with `"passed"` status
- **AND** one or more tasks have `"manual"` status (awaiting approval)
- **THEN** the run status is `"running"`

#### Scenario: Approval completes the run
- **WHEN** the last `"manual"` task is approved and passes
- **THEN** the run status is `"passed"`

#### Scenario: Manual task not reflected in dashboard stats
- **WHEN** querying run stats via `CountRunsByStatus`
- **THEN** no run has status `"manual"`
