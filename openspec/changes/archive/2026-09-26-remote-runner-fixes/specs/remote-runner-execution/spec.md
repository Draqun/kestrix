## MODIFIED Requirements

### Requirement: Incremental output

A runner SHALL be able to append output to a claimed job's trace while it
executes, and Kestrix SHALL make that output visible the same way as
in-process task output. Output SHALL pass through the same secret redaction
applied to every other execution mode before it is stored: Kestrix SHALL
redact every appended chunk against its own configured secrets and every
resolved runtime secret value of the job's run. The claim payload SHALL name
the job's resolved secret values, and a runner SHALL redact its streamed
output against them with a writer that matches a value split across two
writes, before sending any chunk. A trace append SHALL present the claim
epoch it was issued; Kestrix SHALL reject the append without storing it if
the job's current claim epoch no longer matches. Appending SHALL be safe
against concurrent appends: when two jobs of the same run execute on
different runners at once, neither runner's accepted output SHALL be lost or
overwritten by the other's append. Relative ordering between two jobs' chunks
is not guaranteed; not losing them is. Appending SHALL enforce the same
`max_output_bytes` bound as every other mode: once a run's stored output
reaches the bound, further appended content SHALL be dropped and the stored
output SHALL end with the truncation marker, without changing the run's or
any task's status. Redaction SHALL apply before truncation.

#### Scenario: Live output visible during execution
- **WHEN** a runner appends output to a running job's trace
- **THEN** the run's stored output SHALL reflect it before the job completes

#### Scenario: Redaction applies to remote output
- **WHEN** appended output contains the plaintext value of a resolved secret
- **THEN** the stored output SHALL contain the redaction placeholder, not the value

#### Scenario: Runtime secret from secret_ref is redacted
- **WHEN** a task of a remote-mode run prints the value of a variable declared with `secret_ref`
- **THEN** the run's stored output and every API response returning it SHALL contain the redaction placeholder, not the value

#### Scenario: Secret split across two chunks
- **WHEN** a task's output contains a resolved secret value whose bytes a runner would send in two consecutive trace appends
- **THEN** the stored output SHALL contain the redaction placeholder, not either part of the value

#### Scenario: A verbose remote task is truncated at the bound
- **WHEN** a runner appends more trace output than `max_output_bytes` over
  the course of a job
- **THEN** the stored output SHALL be at most the bound plus the truncation
  marker and SHALL end with that marker
- **AND** the job SHALL still complete with the status its runner reports

#### Scenario: Concurrent appends from two jobs of one run
- **WHEN** two runners append trace output at the same time for two different
  jobs of the same run
- **THEN** the run's stored output SHALL contain both appends
- **AND** neither SHALL be overwritten by the other

#### Scenario: Stale claim cannot append trace
- **WHEN** a runner appends output using a claim epoch that has since been
  superseded by a reclaim
- **THEN** the append SHALL be rejected and SHALL NOT be stored

### Requirement: Job completion

A runner SHALL report a claimed job's outcome exactly once: a terminal status,
any remaining output, and, when the task failed, an optional error message.
Kestrix SHALL redact the output and the error the same way as trace output,
SHALL bound the error's length, and SHALL record the error on the task's
result. Kestrix SHALL record the task's result and resume
the run's dependency graph, applying the same blocked/manual/ordering rules
an in-process run applies. A
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

### Requirement: Silent runner recovery

A job left `claimed` or `running` with no trace append and no completion
report past a configured timeout SHALL return to `pending` for another runner
to claim. A job that recovery returned to `pending` and that no runner claims
within the same timeout SHALL count as another unanswered attempt. Kestrix
SHALL count reclaims and unanswered attempts separately from the claim epoch;
a job whose count reaches the configured maximum SHALL fail with an error
naming the reason instead of returning to `pending` again. A job that has
never been claimed SHALL NOT be counted or failed by recovery. Recovery SHALL
NOT return a job to `pending` when its run is already in a terminal status:
such a job SHALL be finalized in place, so cancelling a run is never undone by
a later reclaim.

#### Scenario: Job reclaimed after timeout
- **WHEN** a claimed job receives no update for longer than the configured timeout
- **THEN** it SHALL become `pending` again and be eligible for another runner

#### Scenario: Maximum of one reclaim allows exactly one
- **WHEN** the configured maximum is `1` and a claimed job's runner goes silent past the timeout for the first time
- **THEN** the job SHALL return to `pending`, not fail

#### Scenario: Repeated timeout fails the job
- **WHEN** a job has been reclaimed the configured maximum number of times
- **THEN** it SHALL be marked failed with a message naming that it was abandoned by unresponsive runners

#### Scenario: No runner left
- **WHEN** a job's only runner goes silent mid-job and no runner polls afterwards
- **THEN** the job SHALL fail naming the reason once its count reaches the configured maximum
- **AND** its run SHALL reach a terminal status rather than stay `running`

#### Scenario: Never-claimed job waits
- **WHEN** a job has been `pending` longer than the timeout without ever being claimed
- **THEN** recovery SHALL leave it `pending`

#### Scenario: A canceled run's abandoned job is not reclaimed
- **WHEN** a job goes silent past the timeout and its run has already been marked `canceled`
- **THEN** the job SHALL NOT return to `pending`
- **AND** no runner SHALL receive it on a subsequent poll

## ADDED Requirements

### Requirement: Remote job secrets at rest

Kestrix SHALL NOT store a resolved variable value or resolved secret value of
a remote-mode job or run in plaintext in the database. Such values SHALL be
stored encrypted with the credential master key, bound to the identity of the
row that holds them, and SHALL be deleted once that job or run reaches a
terminal status. On startup, Kestrix SHALL remove plaintext variable values
left in remote job and run records by an earlier version.

#### Scenario: Payload at rest carries no secret
- **WHEN** a remote-mode job whose task uses a `secret_ref` variable is pending
- **THEN** no database column SHALL contain the secret's plaintext value

#### Scenario: Values deleted after completion
- **WHEN** a remote-mode run finishes
- **THEN** its jobs and the run SHALL no longer hold the encrypted variable values

#### Scenario: Legacy plaintext removed
- **WHEN** Kestrix starts against a database whose remote job payloads contain plaintext variable values
- **THEN** those values SHALL be removed before Kestrix serves requests

### Requirement: Claimed remote task reports running

When a runner claims a job, Kestrix SHALL record the job's task as `running`
on the run, the same way an in-process task is recorded when it starts.

#### Scenario: Task shown running while a runner executes it
- **WHEN** a runner has claimed a job and is executing it
- **THEN** the run detail view SHALL show that task as running, not pending

### Requirement: Project execution mode is visible

The project detail page SHALL show the project's execution mode.

#### Scenario: Remote project
- **WHEN** a user opens the detail page of a project in `remote` mode
- **THEN** the page SHALL show that the project's mode is `remote`
