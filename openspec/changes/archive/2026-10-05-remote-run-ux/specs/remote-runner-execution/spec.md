## MODIFIED Requirements

### Requirement: Job completion

A runner SHALL report a claimed job's outcome exactly once: a terminal status,
any remaining output, and, when the task failed, an optional error message.
Kestrix SHALL redact the output and the error the same way as trace output,
SHALL bound the error's length, and SHALL record the error on the task's
result. Kestrix SHALL record the task's result and resume
the run's dependency graph, applying the same blocked/manual/ordering rules
an in-process run applies. The task's result SHALL carry the output of the
claim that completed the job: every trace append Kestrix accepted from that
claim, in order, followed by the completion's remaining output. It SHALL be
redacted and bounded by `max_output_bytes`; past the bound it SHALL be cut
and end with a truncation marker. A
completion report SHALL present the claim epoch it was issued; Kestrix SHALL
accept the first completion report for a given claim epoch and SHALL reject
every subsequent one for that same epoch or for an epoch the job has moved
past, without re-recording the task's result or re-resuming the run's
dependency graph.

A completion report whose run has already reached a terminal status —
`canceled`, `failed` or `passed`, however it got there — SHALL NOT record a
task result and SHALL NOT recompute the run's status. The run's status SHALL
be checked atomically with the claim epoch, in the same transaction that
records the result, so a run that becomes terminal between the two checks
cannot be resumed by a completion that passed the first one. Only the report's
output is kept (see "Cancelling a remote-mode run" for the cancel case; the
same rule covers a run failed by the interrupted-run sweep at startup, which
a runner outlives). A terminal run SHALL NOT return to a non-terminal status
through a late completion.

#### Scenario: Successful completion unblocks dependents
- **WHEN** a runner reports a job passed
- **THEN** the task's result SHALL be recorded
- **AND** any task depending only on it SHALL become eligible to run next

#### Scenario: Failure reason is recorded
- **WHEN** a runner cannot start a task and reports it failed with the error `exec: "bash": executable file not found in $PATH`
- **THEN** the task's result SHALL carry that error
- **AND** the run detail view SHALL show it

#### Scenario: Duplicate completion is a no-op
- **WHEN** a runner reports a job's outcome a second time for a claim epoch
  already completed
- **THEN** the task's result SHALL NOT be recorded again
- **AND** the run's dependency graph SHALL NOT be resumed a second time

#### Scenario: Run becomes terminal while a completion is in flight
- **WHEN** a sibling job fails the run after a completion's claim epoch was
  accepted but before its result is recorded
- **THEN** the completion SHALL NOT record a task result or resume the run's
  dependency graph
- **AND** the run SHALL stay in the terminal status the sibling gave it

#### Scenario: Completion into a terminal run does not revive it
- **WHEN** a runner reports `passed` for a job whose run was already failed by
  the interrupted-run sweep after a Kestrix restart
- **THEN** the task's failed result SHALL stand
- **AND** the run SHALL remain `failed` rather than being recomputed to
  `passed` or `running`

#### Scenario: Completion after reclaim is rejected
- **WHEN** a runner reports a job's outcome using a claim epoch that silent-runner
  recovery has since superseded with a new claim
- **THEN** the completion SHALL be rejected
- **AND** the task's result SHALL come only from whichever claim completes
  first under its own still-current epoch

#### Scenario: A re-queued job's output starts over
- **WHEN** silent-runner recovery returns a job that had streamed output to the queue, and another runner claims it, streams new output and completes it
- **THEN** the task's result SHALL carry only the replacement claim's output
- **AND** the run's aggregate output SHALL contain the output of both claims

#### Scenario: Streamed output appears on the task
- **WHEN** a runner sends a task's output as trace appends and completes the job with no remaining output
- **THEN** the task's result SHALL carry that output
- **AND** the run detail view SHALL show it for that task instead of "(No output)"

#### Scenario: Task output is bounded
- **WHEN** a remote task's accepted trace and remaining output exceed `max_output_bytes`
- **THEN** the task's result output SHALL be at most the bound plus the truncation marker and SHALL end with that marker

## ADDED Requirements

### Requirement: Single-task re-run dispatches only its task

In a `remote`-mode project, a single-task re-run SHALL dispatch exactly one
job, for the named task. Completing that job SHALL NOT dispatch, skip, or
otherwise record any other task of that run. Kestrix SHALL resume a remote
run's dependency graph only for a run that has a remote snapshot to dispatch
from. Approving a manual task inside a whole remote-mode run SHALL still
resume that run's dependents.

#### Scenario: Re-running one task leaves the others alone
- **WHEN** a pipeline has `lint` with trigger `!repo.docs.changed`, `build`, and `deploy` depending on `build`, and an operator re-runs `build` alone in a `remote`-mode project
- **AND** the runner reports `build` passed
- **THEN** no job SHALL be dispatched for `lint` or `deploy`
- **AND** the run's task results SHALL name only `build`

#### Scenario: Approved manual task still resumes its run
- **WHEN** a whole remote-mode run is waiting on a manual task and an operator approves it
- **AND** the runner reports that task passed
- **THEN** the tasks depending on it SHALL become eligible and be dispatched

### Requirement: Rejected runner credentials

A runner-job request with no `Authorization` header, or one that is not a
bearer token, SHALL be rejected with `401` and the message
`missing or malformed Authorization header`. A bearer token Kestrix does not
accept, unknown or revoked alike, SHALL be rejected with `401` and the
message `invalid or revoked runner token`, without revealing which of the
two it is. A failure to check the token SHALL answer `500`, not `401`. The
`kestrix runner` process SHALL exit with a non-zero status, printing the
server's message, when a poll is rejected with `401`. It SHALL keep retrying
every other poll failure.

#### Scenario: Revoked token is named as such
- **WHEN** a runner polls with a token an administrator has revoked
- **THEN** the response SHALL be `401` with the message `invalid or revoked runner token`

#### Scenario: Missing header keeps its message
- **WHEN** a runner-job request has no `Authorization` header
- **THEN** the response SHALL be `401` with the message `missing or malformed Authorization header`

#### Scenario: Store failure is not a credential failure
- **WHEN** Kestrix cannot check a runner token because its store returns an error
- **THEN** the response SHALL be `500`

#### Scenario: A revoked runner stops
- **WHEN** `kestrix runner` polls and receives `401`
- **THEN** it SHALL exit with a non-zero status and print the server's message

#### Scenario: A transient failure does not stop the runner
- **WHEN** `kestrix runner` polls and receives `500` or cannot connect
- **THEN** it SHALL log the error and poll again after its retry delay
