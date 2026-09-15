# run-scheduling Specification

## Purpose

Bounds how much work one Kestrix instance executes at once and how much run history it keeps, so a burst of triggers or weeks of operation cannot exhaust the host or the database.

## Requirements

### Requirement: Concurrent run limit

Kestrix SHALL execute at most `max_concurrent_runs` runs at a time (default `2`; a non-positive or non-integer configured value SHALL fail startup naming the setting). A run accepted while every slot is busy SHALL be recorded with status `queued` and SHALL start automatically, in acceptance order, when a slot frees. Whole-pipeline runs, single-task re-runs, and approved manual tasks each occupy one slot while executing.

#### Scenario: Burst above the limit
- **WHEN** the limit is `2` and three runs are triggered at once
- **THEN** two report `running`, one reports `queued`, and the queued run starts when either of the first two finishes

#### Scenario: Queued run is visible
- **WHEN** a run is `queued`
- **THEN** it appears in `GET /api/runs`, `GET /api/projects/{name}/runs`, the dashboard counters, and the UI with a distinct `queued` state

#### Scenario: Invalid limit
- **WHEN** the configured limit is `0` or `abc`
- **THEN** startup fails with an error naming `runner.max_concurrent_runs`

### Requirement: Cancelling a queued run

Cancelling a `queued` run SHALL mark it `canceled` with the actor recorded and SHALL NOT execute any task; the slot it was waiting for goes to the next queued run.

#### Scenario: Cancel while queued
- **WHEN** a client cancels a `queued` run
- **THEN** the run is `canceled`, no task result is recorded as executed, and the next queued run (if any) starts when a slot frees

### Requirement: Run history retention

After a run reaches a terminal status, Kestrix SHALL delete the oldest terminal runs beyond `run_history_limit` (default `1000`; positive integer, startup fails otherwise). Runs in `running`, `queued`, or awaiting manual approval SHALL never be deleted by retention.

#### Scenario: Limit exceeded
- **WHEN** the limit is `1000` and the 1001st run finishes
- **THEN** the oldest terminal run is deleted and the count of stored runs is `1000`

#### Scenario: Non-terminal runs are protected
- **WHEN** retention runs while some of the oldest runs are `queued` or awaiting manual approval
- **THEN** those runs remain and only older terminal runs are deleted

### Requirement: Run output bound

Run output persisted for a run SHALL NOT exceed `max_output_bytes` (default 10 MiB; positive integer). Output beyond the bound SHALL be dropped and the stored output SHALL end with a marker line stating that it was truncated. Truncation SHALL NOT change the run's or any task's status, and secret redaction SHALL apply before truncation.

#### Scenario: Verbose task
- **WHEN** a task writes more than the bound
- **THEN** the stored output is at most the bound plus the marker line, ends with the marker, and the run status reflects the task's exit code as usual
