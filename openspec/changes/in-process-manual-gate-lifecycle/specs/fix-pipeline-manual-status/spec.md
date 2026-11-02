# Spec Delta: fix-pipeline-manual-status

## ADDED Requirements

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
