# dashboard-run-navigation Specification

## Purpose
TBD - created by archiving change dashboard-run-navigation. Update Purpose after archive.
## Requirements
### Requirement: Recent runs row navigates to run detail
The system SHALL navigate to the run detail page when a user clicks a row in the dashboard's recent runs table.

#### Scenario: Click row navigates to run detail
- **WHEN** user clicks a recent runs table row
- **THEN** the browser navigates to `/pipelines/runs/{projectName}/{runId}`

#### Scenario: Row shows pointer cursor on hover
- **WHEN** user hovers over a recent runs table row
- **THEN** the cursor changes to a pointer

