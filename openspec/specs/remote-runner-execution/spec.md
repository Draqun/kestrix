# remote-runner-execution Specification

## Purpose
Lets a task execute on a machine other than the Kestrix host: a runner
registers with Kestrix, polls for work, executes with its own locally
configured engine, and streams output and a result back.
## Requirements
### Requirement: Runner registration

An administrator SHALL be able to register a runner by name and an optional
set of tags and receive a bearer token, shown once. The token SHALL be stored
only in hashed form. A runner SHALL present this token on every call to the
runner-job endpoints. A runner's tags SHALL be assigned only at registration
by the administrator; no runner-job endpoint SHALL accept tags from the
runner. An administrator SHALL be able to revoke a runner, immediately
invalidating its token.

#### Scenario: Token shown once
- **WHEN** an administrator registers a new runner
- **THEN** the response SHALL include the token
- **AND** no later API response SHALL ever return that token again

#### Scenario: Revocation is immediate
- **WHEN** an administrator revokes a runner
- **THEN** a subsequent call using that runner's token SHALL be rejected with `401`

#### Scenario: Tags normalised at registration
- **WHEN** an administrator registers a runner with tags `["GPU", " gpu", "linux"]`
- **THEN** the runner SHALL be stored with tags `["gpu", "linux"]`

#### Scenario: Invalid tag rejected
- **WHEN** an administrator registers a runner with a tag outside `[a-z0-9._-]{1,64}` after normalisation
- **THEN** the request SHALL be rejected with `400` naming the tag
- **AND** no runner SHALL be created

### Requirement: Remote project mode

A project's `Mode` SHALL accept the value `remote` alongside `local`,
`docker`, and `gitlab-runner`. In a `remote`-mode project, every task of a run
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

### Requirement: Job polling

A runner SHALL be able to poll for the next available job. The poll SHALL
block for a bounded interval and return `204` when no job is available, or
`200` with a complete job payload — the task's script, its resolved pipeline
variables, a prepared source archive (the same checkout Kestrix already
acquires server-side for every other mode, packaged for transfer), and a
claim epoch identifying this specific claim — when one is claimed for it. The
payload SHALL NOT contain repository credentials: the runner receives files,
not the means to fetch them itself. A job already claimed by another runner
SHALL NOT be returned to a second poller. A job whose run is in a terminal
status SHALL NOT be offered to any poller, regardless of the job's own
status. Every claim, including a reclaim of the same job after silent-runner
recovery, SHALL receive a new, distinct claim epoch.

#### Scenario: Job claimed exactly once
- **WHEN** two runners poll at the same moment and one pending job exists
- **THEN** exactly one of them SHALL receive it
- **AND** the other SHALL receive `204` or the next distinct pending job

#### Scenario: No work available
- **WHEN** no job is pending
- **THEN** a poll SHALL return `204` after the bounded wait, not an error

### Requirement: Runner-job request bounds

The runner-job route group SHALL bound the size of request bodies it
receives, independently of `/api`'s bound, the same way `/webhook` bounds its
own body size independently of `/api`. An inbound request body — a
trace-append chunk or a completion report — exceeding the configured limit
SHALL be rejected rather than accepted unbounded. The bound applies to what a
runner sends, not to the job payload Kestrix sends back from a poll: that
payload carries the source archive and is deliberately unbounded here (see
design.md Risks, which accepts a large archive at PoC scale and names
fetch-by-reference as the upgrade path). Kestrix SHALL also bound the number
of concurrent long-poll connections a single runner token may hold open
against `GET /api/runner-jobs/next`.

#### Scenario: Oversized trace chunk rejected
- **WHEN** a runner appends a trace chunk larger than the configured limit
- **THEN** the request SHALL be rejected and the chunk SHALL NOT be stored

#### Scenario: Excess concurrent polls from one token rejected
- **WHEN** a single runner token already holds the configured maximum number
  of open long-poll connections
- **THEN** an additional poll from that same token SHALL be rejected rather
  than accepted unbounded

### Requirement: Runner selects its own execution engine

Kestrix SHALL NOT dictate how a runner executes a job. A runner's own local
configuration SHALL decide whether a job runs in a container or as a bare
process. Kestrix's job payload SHALL contain no field that names a container
engine or image.

#### Scenario: Two differently configured runners
- **WHEN** one registered runner is configured for Docker execution and another for bare-shell execution
- **THEN** each SHALL execute a claimed job with its own configured engine
- **AND** Kestrix SHALL treat both results identically

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

### Requirement: Cancelling a remote-mode run

Kestrix SHALL NOT send a stop signal to a runner: the runner-job protocol is
poll-only, with no inbound channel from Kestrix to a runner (design.md D8),
so there is no transport to deliver one. Canceling a
remote-mode run SHALL prevent its `pending` jobs from being claimed and SHALL
mark the run `canceled` without waiting for jobs already `claimed` or
`running`. A completion report for a job belonging to an already-`canceled`
run SHALL be accepted so its remaining output is not lost, and that output
SHALL be stored against the run's trace. It SHALL NOT be recorded as a task
result: cancellation already finalized that task to `canceled`, and that
record SHALL remain the task's surviving outcome. Such a report SHALL NOT
resume the run's dependency graph or change the run's terminal status.

#### Scenario: Cancel prevents pending jobs from being claimed
- **WHEN** a remote-mode run is canceled while one of its jobs is still `pending`
- **THEN** that job SHALL NOT be claimed by any runner afterward

#### Scenario: Cancel does not stop a running job
- **WHEN** a remote-mode run is canceled while one of its jobs is `claimed` or `running`
- **THEN** Kestrix SHALL mark the run `canceled` without sending any signal to
  the runner executing that job

#### Scenario: Late completion after cancel does not resume the run
- **WHEN** a runner reports a job's completion after its run has already been marked `canceled`
- **THEN** the run's dependency graph SHALL NOT resume
- **AND** the run's status SHALL remain `canceled`

#### Scenario: Late completion does not overwrite the canceled task result
- **WHEN** a runner reports `passed` for a job whose task was finalized to
  `canceled` when the run was cancelled
- **THEN** the task's surviving result SHALL still be `canceled`
- **AND** the report's output SHALL still be stored against the run's trace

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

### Requirement: Runner liveness tracking

Kestrix SHALL record the time of a runner's most recent authenticated request
to any runner-job endpoint and SHALL report each runner's liveness as
`busy` when it holds a claimed job, otherwise `online` when that time is
within the configured staleness threshold (`KESTRIX_RUNNER_OFFLINE_AFTER`,
default `60s`, a positive duration), `offline` when it is older, and
`never seen` when the runner has made no such request. A request with an
unknown or revoked token SHALL NOT update any runner's recorded time.

#### Scenario: Runner goes offline
- **WHEN** a runner holding no claimed job has not made a runner-job request within the staleness threshold
- **THEN** it SHALL be reported `offline`

#### Scenario: Polling keeps a runner online
- **WHEN** a runner polls for a job and receives none
- **THEN** it SHALL be reported `online` immediately afterwards

#### Scenario: Quiet job does not mark a runner offline
- **WHEN** a runner has claimed a job and sent no request for longer than the staleness threshold
- **THEN** it SHALL be reported `busy`, not `offline`

#### Scenario: Newly registered runner
- **WHEN** a runner has been registered but has never polled
- **THEN** it SHALL be reported `never seen`

#### Scenario: Invalid threshold rejected at startup
- **WHEN** `KESTRIX_RUNNER_OFFLINE_AFTER` is not a positive duration
- **THEN** startup SHALL fail naming the setting

### Requirement: Runner tags and task matching

A runner SHALL be able to carry a set of tags assigned at registration. A task
SHALL be able to declare the tags it requires. A job SHALL only be offered to
a runner whose tags are a superset of the job's required tags. A job's
required tags SHALL be the task's tags at the time the job was dispatched. A
job with no required tags SHALL be offered to any runner, including a tagged
one. A job no polling runner matches SHALL NOT delay the offering of other
pending jobs to that runner.

#### Scenario: Job withheld from an unmatched runner
- **WHEN** a task requires a tag no polling runner currently has registered
- **THEN** the job SHALL remain `pending` rather than being offered to a non-matching runner

#### Scenario: Superset runner receives the job
- **WHEN** a task requires `["gpu"]` and a runner tagged `["gpu", "linux"]` polls
- **THEN** that runner SHALL be offered the job

#### Scenario: Untagged job runs anywhere
- **WHEN** a task declares no runner tags and a runner tagged `["deploy"]` polls
- **THEN** that runner SHALL be offered the job

#### Scenario: Unmatched job does not block the queue
- **WHEN** the oldest pending job requires a tag the polling runner lacks and a newer pending job requires none
- **THEN** the polling runner SHALL be offered the newer job

### Requirement: Runner management interface

Kestrix SHALL provide an administrator-only API listing registered, unrevoked
runners with their name, tags, creation time, last-seen time, and liveness,
never including a token or token hash. Kestrix SHALL provide a Settings page,
visible only to administrators, that lists those runners, registers a runner
(showing its token once), and revokes a runner after confirmation.

#### Scenario: Revoke from the UI
- **WHEN** an administrator revokes a runner from the Settings page
- **THEN** the runner's token SHALL be invalidated the same as an API-driven revocation

#### Scenario: Non-administrator cannot list runners
- **WHEN** an account below the admin role requests the runner list
- **THEN** the request SHALL be rejected with `403`

#### Scenario: Token never listed
- **WHEN** an administrator lists runners
- **THEN** no entry SHALL contain a token or token hash

#### Scenario: Register from the UI
- **WHEN** an administrator registers a runner from the Settings page
- **THEN** the page SHALL show the new token once, with the command to start `kestrix runner` using it
- **AND** after the dialog is closed the token SHALL NOT be shown again

