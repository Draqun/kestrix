# fix-pipeline-manual-status Specification

## Purpose
Specifies manual gate lifecycle, task dependency blocking, and overall run status resolution during pipeline execution.

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

### Requirement: Dependents of a waiting manual gate wait in in-process mode
In in-process execution (`local` and `docker`), tasks that depend on an unapproved task in `"manual"` status SHALL remain in `"pending"` status until the manual task is approved and completes with `"passed"`.

#### Scenario: Downstream task blocked by manual gate
- **WHEN** task `deploy` is awaiting approval with status `"manual"`
- **AND** task `notify` has `depends_on = ["deploy"]`
- **THEN** `notify` remains in `"pending"` status and is not executed until `deploy` passes

### Requirement: Failed dependency marks in-process manual gates as skipped
In in-process execution, if any upstream dependency of a manual gate fails, is canceled, or is skipped, the manual gate SHALL transition to `"skipped"` and SHALL NOT block run completion.

#### Scenario: Build failure skips downstream manual gate
- **WHEN** task `build` finishes with status `"failed"`
- **AND** manual task `deploy` depends on `build`
- **THEN** `deploy` transitions to `"skipped"` with reason `dependency build failed`
- **AND** the run status finalizes as `"failed"`
