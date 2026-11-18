# dashboard-run-navigation Specification

## Purpose
Enforces intuitive dashboard navigation by linking recent run table rows directly to their detailed pipeline run views.
## Requirements
### Requirement: Recent runs row navigates to run detail
The system SHALL navigate to the run detail page when a user clicks a row in the dashboard's recent runs table.

#### Scenario: Click row navigates to run detail
- **WHEN** user clicks a recent runs table row
- **THEN** the browser navigates to `/pipelines/runs/{projectName}/{runId}`

#### Scenario: Row shows pointer cursor on hover
- **WHEN** user hovers over a recent runs table row
- **THEN** the cursor changes to a pointer

