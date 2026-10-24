# run-scheduling Specification

## Purpose

Bounds how much work one Kestrix instance executes at once and how much run history it keeps, so a burst of triggers or weeks of operation cannot exhaust the host or the database.
## Requirements
### Requirement: Concurrent run limit

Kestrix SHALL execute at most `max_concurrent_runs` runs at a time (default `2`; a non-positive or non-integer configured value SHALL fail startup naming the setting). A run accepted while every slot is busy SHALL be recorded with status `queued` and SHALL start automatically, in acceptance order, when a slot frees. Whole-pipeline runs, single-task re-runs, and approved manual tasks each occupy one slot while executing. Acceptance SHALL be reported before a slot is acquired: every route that starts a run SHALL respond with the run's id and its accepted status (`running` or `queued`) without waiting for the run to start or finish, and the run's outcome SHALL be read from the run record afterwards. A `remote`-mode run's tasks SHALL NOT occupy an in-process concurrency slot: their pacing is bounded by registered-runner availability, not `max_concurrent_runs`.

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

Kestrix SHALL store the output and the error of a run, and the output and the
error of each of its task results, as valid UTF-8 without NUL bytes, whichever
path wrote them: an in-process run, an approved manual task, a single-task
re-run, or the trace and completion of a remote job. Each consecutive run of
bytes that are not valid UTF-8 or are NUL SHALL be stored as one U+FFFD, after
secret redaction. Such bytes SHALL NOT change the status of the run or of the
task that printed them, and SHALL NOT keep a run from reaching its terminal
status.

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

