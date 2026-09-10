## MODIFIED Requirements

### Requirement: Cancellation terminates task processes

Cancelling a run SHALL stop the run's executing task before its status is
recorded as `canceled`, regardless of how the run's task was started: a
full-DAG run, a single-task re-run, or an approved manual task all use the
same termination path. In `local` mode the task's whole process group SHALL
receive a termination signal, then a kill signal after
`KESTRIX_RUNNER_CANCEL_GRACE` (default `10s`). In `docker` mode the task
container SHALL be stopped with the same grace period. Tasks not yet started
SHALL NOT start. A run whose process could not be terminated SHALL stay
`running` with the failure in its error, rather than being reported as
`canceled`.

#### Scenario: Cancel a running local task

- **WHEN** a run is cancelled while a `local` task whose script spawned child processes is executing
- **THEN** the task process and every process in its group SHALL exit within the grace period plus the kill
- **AND** the run SHALL be `canceled` and no later task in the DAG SHALL start

#### Scenario: Cancel a running Docker task

- **WHEN** a run is cancelled while a `docker` task is executing
- **THEN** the task's container SHALL no longer be running after the grace period
- **AND** the run SHALL be `canceled`

#### Scenario: Cancel with no running process

- **WHEN** a run is cancelled while it waits for manual approval and no task process exists
- **THEN** the run SHALL be `canceled` as before with no termination attempted

#### Scenario: Termination fails

- **WHEN** the runtime rejects the stop request for a task container
- **THEN** the run SHALL remain `running`
- **AND** the cancel request SHALL fail with the runtime's error, without exposing host paths or secrets

#### Scenario: Cancel a running single-task re-run

- **WHEN** a run is cancelled while a task started by the single-task re-run route is executing
- **THEN** the task's process group SHALL receive a termination signal, then a kill signal after the grace period, by the same rule as a `local`-mode DAG task
- **AND** the run SHALL be `canceled`

#### Scenario: Cancel an executing approved manual task

- **WHEN** a manual task has been approved and its process is executing, and the run is cancelled before that process exits
- **THEN** the task's process group SHALL receive a termination signal, then a kill signal after the grace period, by the same rule as a `local`-mode DAG task
- **AND** the run SHALL be `canceled` rather than left `running` with the process still active
