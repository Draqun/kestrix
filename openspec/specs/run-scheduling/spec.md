# run-scheduling Specification

## Purpose

Bounds how much work one Kestrix instance executes at once and how much run history it keeps, so a burst of triggers or weeks of operation cannot exhaust the host or the database.

## Requirements

### Requirement: Concurrent run limit

Kestrix SHALL execute at most `max_concurrent_runs` in-process runs at once (default `2`; non-positive/non-integer values fail startup). Runs accepted when all slots are busy SHALL be recorded as `queued` and start in order as slots free. Start routes SHALL return immediately with the run ID and status (`running` or `queued`). Remote-mode tasks SHALL NOT consume in-process slots.

#### Scenario: Burst above the limit
- **WHEN** the limit is `2` and three runs are triggered at once
- **THEN** two report `running`, one reports `queued`, and the queued run starts when either of the first two finishes

#### Scenario: Queued run is visible
- **WHEN** a run is `queued`
- **THEN** it appears in `GET /api/runs`, `GET /api/projects/{name}/runs`, the dashboard counters, and the UI with a distinct `queued` state

#### Scenario: Invalid limit
- **WHEN** the configured limit is `0` or `abc`
- **THEN** startup fails with an error naming `runner.max_concurrent_runs`

#### Scenario: Single-task re-run is accepted immediately
- **WHEN** a single-task re-run is requested while every slot is busy
- **THEN** the response SHALL carry the new run's id and the status `queued`
- **AND** the response SHALL NOT wait for the task to execute or carry its output

#### Scenario: Remote-mode tasks bypass the concurrency slot
- **WHEN** `max_concurrent_runs` in-process slots are all busy and a `remote`-mode run starts
- **THEN** its tasks SHALL still dispatch as jobs immediately
- **AND** SHALL NOT be recorded `queued` waiting for an in-process slot

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

### Requirement: Run output is stored as valid text

Kestrix SHALL sanitize and store all run and task output/error text as valid UTF-8 without NUL bytes across all execution modes. Any invalid UTF-8 sequences or NUL bytes SHALL be replaced with `U+FFFD` after secret redaction. Text sanitization SHALL NOT alter task exit codes or run terminal states.

#### Scenario: A task prints a byte that is not UTF-8
- **WHEN** an in-process task prints `caf` followed by the byte `0xE9`, then a newline, and passes
- **THEN** the run SHALL become `passed`
- **AND** its output SHALL contain `caf`, U+FFFD and the newline

#### Scenario: A task prints a NUL byte
- **WHEN** a task prints `a`, a NUL byte and `b`
- **THEN** the output of the run and of the task result SHALL contain `a`, U+FFFD and `b`

#### Scenario: The output bound cuts a multi-byte character
- **WHEN** `max_output_bytes` falls inside a multi-byte character of a task's output
- **THEN** the run SHALL still reach its terminal status
- **AND** the stored output SHALL end with the truncation marker

#### Scenario: A remote runner reports a NUL byte
- **WHEN** a runner's trace append or completion for a job carries a NUL byte
- **THEN** Kestrix SHALL accept it
- **AND** store the text with U+FFFD in place of the NUL byte

#### Scenario: A secret is redacted before replacement
- **WHEN** a resolved secret contains a byte that is not valid UTF-8 and a task prints it
- **THEN** the stored output SHALL NOT contain the secret

### Requirement: Automatically cancel superseded runs

When a run starts for a project with `cancel_in_progress = true` and an identified branch or ref, Kestrix SHALL automatically cancel every prior non-terminal run for that same project and branch. The cancellation SHALL record actor `system:cancel-in-progress`. Active in-process tasks SHALL be terminated within the cancellation grace period, unclaimed remote jobs SHALL be marked unclaimable, and queued runs SHALL transition to `canceled` without executing tasks.

#### Scenario: Running run on the same branch is canceled
- **WHEN** a run is `running` on branch `feature-x` for a project with `cancel_in_progress = true` and a new run starts on branch `feature-x`
- **THEN** the earlier run SHALL transition to `canceled` with `canceled_by` set to `system:cancel-in-progress`
- **AND** the new run SHALL continue execution

#### Scenario: Queued run on the same branch is canceled without executing
- **WHEN** a run is `queued` on branch `feature-x` for a project with `cancel_in_progress = true` and a new run starts on branch `feature-x`
- **THEN** the queued run SHALL transition to `canceled` with `canceled_by` set to `system:cancel-in-progress`
- **AND** the slot it waited for SHALL NOT be occupied by that canceled run

#### Scenario: Different branches are not canceled
- **WHEN** a run is `running` on branch `main` and a new run starts on branch `feature-y` for the same project
- **THEN** the run on `main` SHALL NOT be canceled and SHALL continue execution

#### Scenario: Cancel in progress disabled preserves concurrent runs
- **WHEN** a project has `cancel_in_progress = false` or omitted and multiple runs start for the same branch
- **THEN** earlier runs SHALL continue running or remain queued in FIFO order without automatic cancellation

#### Scenario: Remote-mode run marks unclaimed jobs unclaimable
- **WHEN** a `remote`-mode run on branch `feature-z` is superseded by a newer run on branch `feature-z`
- **THEN** its unclaimed pending jobs SHALL be marked unclaimable and the run SHALL transition to `canceled`

#### Scenario: Manual whole-pipeline run cancels prior runs on the same primary branch
- **WHEN** a run is `running` on branch `feature-x` for a project with `cancel_in_progress = true` and a user manually starts a whole-pipeline run on branch `feature-x`
- **THEN** the earlier run SHALL transition to `canceled` with `canceled_by` set to `system:cancel-in-progress`
- **AND** the new manual run SHALL continue execution

#### Scenario: Single-task reruns or triggers without primary ref do not cancel active runs
- **WHEN** a single-task rerun or trigger without primary source ref starts for a project with `cancel_in_progress = true`
- **THEN** existing active runs SHALL NOT be canceled and SHALL continue execution
