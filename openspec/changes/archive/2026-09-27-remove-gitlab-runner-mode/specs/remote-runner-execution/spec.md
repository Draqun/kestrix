## MODIFIED Requirements

### Requirement: Remote project mode

A project's `Mode` SHALL accept the value `remote` alongside `local` and
`docker`. In a `remote`-mode project, every task of a run
SHALL be dispatched as a job for a registered runner instead of executing
in-process. This SHALL hold for every entry point that executes a task — a
whole-pipeline run, a single-task re-run, and an approved manual task alike —
not only for a run starting.

#### Scenario: Remote-mode run creates jobs, not in-process execution
- **WHEN** a run starts for a project with `Mode: remote`
- **THEN** each of its tasks SHALL become a pending job
- **AND** no task of that run SHALL execute inside the Kestrix process

#### Scenario: Single-task re-run dispatches a job
- **WHEN** a single task is re-run for a project with `Mode: remote`
- **THEN** that task SHALL become a pending job
- **AND** it SHALL NOT execute inside the Kestrix process
- **AND** the run SHALL NOT be recorded as finished before a runner reports
  the task's outcome

#### Scenario: Approved manual task dispatches a job
- **WHEN** an administrator approves a manual task of a `remote`-mode project's run
- **THEN** that task SHALL become a pending job for a runner to claim
- **AND** it SHALL NOT execute inside the Kestrix process
