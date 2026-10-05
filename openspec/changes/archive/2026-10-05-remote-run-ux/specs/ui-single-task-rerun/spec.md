## ADDED Requirements

### Requirement: A single-task re-run is labelled as one

A run started by re-running one task SHALL record that task's name as the
run's re-run task, returned as `rerun_task` by every endpoint that returns
the run, and SHALL record no changed repositories. The run list and the run
detail view SHALL label such a run "Re-run: <task>" and SHALL NOT show it as
"Changed: <task>". This SHALL hold in every project mode. A run that is not a
single-task re-run SHALL have an empty `rerun_task` and keep its existing
changed-repository label.

#### Scenario: Re-run is labelled with its task
- **WHEN** an operator re-runs the finished task `build`
- **THEN** the new run SHALL have `rerun_task` `build` and an empty `changed`
- **AND** the run list and run detail view SHALL show "Re-run: build"

#### Scenario: A whole run keeps its changed label
- **WHEN** a webhook starts a run with repository `app` changed
- **THEN** the run SHALL have an empty `rerun_task`
- **AND** the run detail view SHALL show "Changed: app"
