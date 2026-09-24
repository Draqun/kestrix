## Purpose

Lets an operator re-run one task of a finished run from the run detail view,
instead of re-running the whole pipeline or calling the API by hand.

## ADDED Requirements

### Requirement: Re-run action on a finished task

The run detail view SHALL offer a re-run action for each task of a run that has
reached a terminal status, when the task itself finished as `passed`, `failed`,
or `canceled`. A task that never ran - `skipped` because a dependency failed or
was canceled, or waiting for manual approval - SHALL NOT be offered it. The
action SHALL be available from the job list and from the header of the selected
job. Activating it SHALL start a new run containing only that task.

#### Scenario: Re-run a failed task
- **WHEN** an operator opens a finished run and activates re-run on a failed task
- **THEN** a new run SHALL be created containing only that task
- **AND** the original run SHALL be left unchanged

#### Scenario: Action absent while the run is active
- **WHEN** the run is `running` or `queued`
- **THEN** no re-run action SHALL be offered for any of its tasks

#### Scenario: No action for a task that never ran
- **WHEN** a finished run has a task `skipped` because a dependency failed
- **THEN** no re-run action SHALL be offered for that task

#### Scenario: Manual approval is a separate action
- **WHEN** a task is waiting for manual approval
- **THEN** the existing approval action SHALL be shown for it
- **AND** the re-run action SHALL NOT replace or duplicate it

### Requirement: Re-run is restricted to operators

The re-run action SHALL be visible and usable only to an account holding at
least the operator role. A viewer SHALL NOT see it, and a viewer's direct
request to the underlying route SHALL be rejected.

#### Scenario: Viewer sees no action
- **WHEN** an account with the viewer role opens a finished run
- **THEN** no re-run action SHALL be rendered

#### Scenario: Operator sees the action
- **WHEN** an account with the operator role or above opens a finished run
- **THEN** the re-run action SHALL be rendered for each task that finished as
  `passed`, `failed`, or `canceled`

### Requirement: Re-run follows the new run

After the re-run is accepted, the view SHALL move the user to the newly created
run and SHALL show its live status. The user SHALL NOT be left on the previous
run with no indication that a new one started.

#### Scenario: Navigation after re-run
- **WHEN** a re-run is accepted
- **THEN** the view SHALL show the new run, identified by the run id returned by the API

#### Scenario: Queued re-run
- **WHEN** the new run is accepted while every concurrency slot is busy
- **THEN** the view SHALL show it in the `queued` state rather than waiting for it to start

#### Scenario: Rejected re-run
- **WHEN** the API rejects the re-run
- **THEN** the error message SHALL be shown on the current run
- **AND** the view SHALL stay on the current run
