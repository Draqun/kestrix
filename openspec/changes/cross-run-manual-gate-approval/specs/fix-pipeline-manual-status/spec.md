# Spec Delta: fix-pipeline-manual-status

## ADDED Requirements

### Requirement: Cross-run manual gate approval
The system SHALL permit approval of a manual gate when all of its declared dependencies are satisfied, including dependencies satisfied in preceding runs on the same branch when absent from the current run.

#### Scenario: Manual gate with prior-run dependency
- **WHEN** a task `deploy` is manual and depends on `build`
- **AND** `build` is absent from the current run's task results but passed in a prior run on the same branch
- **THEN** `isManualWaiting` evaluates to true in the UI
- **AND** `POST /api/projects/{name}/runs/{id}/tasks/{task}/run` succeeds
