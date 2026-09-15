# run-lifecycle-recovery Specification

## Purpose

Guarantees that a run cannot remain reported as executing after the process that executed it is gone, so run lists, counters, and guards reflect reality after a restart.

## Requirements

### Requirement: Interrupted runs are failed on startup

On startup, before accepting requests, Kestrix SHALL mark every run whose status is `running` and which has at least one task result in `running` or `pending` as `failed` with the error `interrupted by server restart` and a set `finished_at`. Each such task result SHALL be marked `failed` (if it was `running`) or `skipped` (if it was `pending`) with the same error. The run output SHALL end with a line stating the interruption.

#### Scenario: Run with an executing task
- **WHEN** the server starts and a run has a task result in `running`
- **THEN** the run is `failed` with error `interrupted by server restart`, that task is `failed`, and any `pending` task is `skipped`

#### Scenario: Run whose next task had not started
- **WHEN** the server starts and a run has only `passed` and `pending` task results
- **THEN** the run is `failed` and the `pending` tasks are `skipped` with the interruption error

#### Scenario: Recovery is visible in the API
- **WHEN** a client fetches an interrupted run after startup
- **THEN** `status` is `failed`, `error` is `interrupted by server restart`, and the output's last line names the interruption

### Requirement: Runs awaiting manual approval survive a restart

A run whose only non-terminal task results are `manual` SHALL NOT be modified by startup reconciliation and SHALL remain approvable.

#### Scenario: Manual gate pending across restart
- **WHEN** the server restarts while a run waits on a `manual` task and every other task is terminal
- **THEN** the run still reports `running`, the task still reports `manual`, and approving it executes the task

### Requirement: Reconciliation is idempotent and reported

Running the reconciliation when nothing is interrupted SHALL change no row. Each startup SHALL log one line with the count of runs failed by reconciliation, including zero.

#### Scenario: Clean restart
- **WHEN** the server starts with no `running` runs
- **THEN** no run is modified and the log reports zero interrupted runs

#### Scenario: Second start after recovery
- **WHEN** the server starts again after a reconciliation already ran
- **THEN** the previously failed runs are unchanged and the count logged is zero
