## ADDED Requirements

### Requirement: A remote run finishes after its dependents are decided

Kestrix SHALL NOT give a remote-mode run a terminal status because of a job's
completion reported by a runner until it has decided what that completion
unblocks: dispatched the tasks that became due, or recorded them `skipped`
because a dependency failed. Until then the run SHALL stay `running`. Once no
task is pending or running, the run SHALL take the status its results imply,
unless Kestrix cannot decide or record that status. Kestrix SHALL NOT resume a
run that is already finished. If Kestrix cannot create the job of a task that became
due, it SHALL record that task as `failed` with an error naming the failure,
so the run ends `failed` rather than staying `running` with a task that has no
job.

Two failures leave the run `running` although its results hold nothing
pending: a walker that stops before it knows what was due - the run, its
pipeline or its snapshot cannot be read - and a finishing write that fails
every retry. Both keep the run visible and cancelable, and the startup sweep
marks it `failed` (`interrupted by server restart`) when the process restarts.

This requirement covers a completion reported by a runner. Silent-runner
recovery is outside it: a job that exhausts its reclaims is failed by the
recovery sweep, which ends its run in the same transaction when the run's
results leave nothing in flight, so the tasks that job blocked are neither
dispatched nor recorded `skipped`.

A manual gate is outside it as well: in a remote run a manual task is never
dispatched and no result is recorded for it, so the run takes the status its
other results imply. A dependent waits for every one of its dependencies to
be decided, so a dependency that is never decided - a manual gate - leaves
the dependent neither dispatched nor recorded.

#### Scenario: A dependent unblocked by the last running task runs
- **WHEN** a whole run has `build` and `deploy`, `deploy` depends on `build`, and the runner reports `build` passed
- **THEN** the run SHALL still be `running`
- **AND** a job for `deploy` SHALL be available to a runner
- **AND** when the runner reports `deploy` passed, the run SHALL become `passed`

#### Scenario: A failure skips the dependents and fails the run
- **WHEN** the runner reports `build` failed in that run
- **THEN** `deploy` SHALL be recorded `skipped`
- **AND** the run SHALL become `failed`

#### Scenario: A single-task re-run still finishes
- **WHEN** the runner reports the only job of a single-task re-run passed
- **THEN** the run SHALL become `passed`

#### Scenario: Concurrent completions do not finish the run early
- **WHEN** `lint` and `build` of one run are reported passed at the same moment and `deploy` depends on `build`
- **THEN** `deploy` SHALL be dispatched exactly once
- **AND** the run SHALL stay `running` until `deploy` completes

#### Scenario: A dependent that cannot be dispatched fails
- **WHEN** a task becomes due and Kestrix cannot create its job
- **THEN** that task SHALL be recorded `failed` with an error naming the failure
- **AND** the run SHALL become `failed`

#### Scenario: A run canceled before it is resumed stays canceled
- **WHEN** a run is canceled after a job's completion was recorded and before Kestrix resumed the run
- **THEN** no job SHALL be dispatched for the run
- **AND** the run SHALL stay `canceled`
