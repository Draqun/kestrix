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

### Requirement: Job polling

Runners SHALL poll for jobs via long-polling, receiving HTTP 204 when empty or HTTP 200 with a complete payload (script, resolved variables, source archive, unique claim epoch). Payloads SHALL NOT contain repo credentials. Jobs already claimed or belonging to terminal runs SHALL NOT be offered to pollers. Each claim or reclaim SHALL receive a distinct claim epoch.

#### Scenario: Job claimed exactly once
- **WHEN** two runners poll at the same moment and one pending job exists
- **THEN** exactly one of them SHALL receive it
- **AND** the other SHALL receive `204` or the next distinct pending job

#### Scenario: No work available
- **WHEN** no job is pending
- **THEN** a poll SHALL return `204` after the bounded wait, not an error

### Requirement: Runner-job request bounds

The runner-job endpoints SHALL independently bound inbound request bodies (trace chunks and completion reports), rejecting oversized requests with HTTP 413. Outbound source archive payloads SHALL NOT be constrained by this request bound. The server SHALL also bound concurrent long-poll connections per runner token.

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

Runners SHALL stream trace output chunks presenting the current claim epoch, with Kestrix validating epochs and redacting configured and runtime secrets before persisting. Output appends SHALL be concurrent-safe across parallel jobs, enforce `max_output_bytes` truncation with marker lines, and reject stale claim epochs without data loss.

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

### Requirement: Runner offline trace spooling

A runner SHALL queue outbound trace chunks in a bounded local trace spool whenever network delivery fails due to connection loss, network timeout, or server error. The runner SHALL retry delivery in strict sequential offset order using backoff. Before reporting job completion, the runner SHALL attempt to flush all spooled trace chunks. If the spool exceeds the configured capacity, the runner SHALL truncate oldest buffered chunks while maintaining valid sequential byte offsets.

#### Scenario: Network interruption spools trace chunks
- **WHEN** delivery of a trace chunk fails due to a network connection loss
- **THEN** the runner SHALL buffer the unsent chunk in its local spool
- **AND** subsequent output writes SHALL append to the spool in consecutive byte-offset order
- **AND** the running task script SHALL NOT be terminated

#### Scenario: Reconnected runner flushes spool in sequential order
- **WHEN** the runner regains network connectivity to Kestrix
- **THEN** the runner SHALL send all buffered trace chunks in increasing offset order
- **AND** the server SHALL accept and record the contiguous trace output without offset mismatch errors

#### Scenario: Completion flushes pending spooled chunks
- **WHEN** a task script exits while uncommitted chunks remain in the trace spool
- **THEN** the runner SHALL flush all spooled chunks to Kestrix before sending the job completion request

#### Scenario: Bounded spool limit truncation
- **WHEN** unsent trace chunks exceed the runner spool limit `KESTRIX_RUNNER_TRACE_SPOOL_MAX_BYTES`
- **THEN** the runner SHALL drop the oldest buffered chunks, insert a truncation marker, and preserve monotonic byte offsets

### Requirement: Job completion

Runners SHALL report job completion once with terminal status, remaining output, and optional errors for its current claim epoch. Kestrix SHALL redact output/errors, record the task result, and resume DAG resolution atomically. Reports for already-terminal runs or stale/duplicate claim epochs SHALL be rejected without re-evaluating run status or task outcomes.

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

### Requirement: Cancelling a remote-mode run

Cancelling a remote run SHALL immediately mark the run and its pending jobs `canceled` without signalling poll-only remote workers. Completions arriving after cancellation SHALL append output to the run trace for observability but SHALL NOT alter finalized task results or resume dependency graphs.

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

Jobs abandoned without heartbeats or trace appends past timeouts SHALL be reset to `pending` with separate retry counters. Exceeding configured reclaim limits SHALL transition the job to `failed`. Recovery SHALL NOT reclaim jobs whose runs reached terminal status.

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

Kestrix SHALL record timestamps of authenticated runner requests, reporting status as `busy` (holding jobs), `online` (seen within `KESTRIX_RUNNER_OFFLINE_AFTER`, default 60s), `offline`, or `never seen`. Invalid or revoked tokens SHALL NOT update liveness.

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

Runner endpoints SHALL reject missing or malformed headers with HTTP 401 (`missing or malformed Authorization header`), and unknown or revoked tokens uniformly with HTTP 401 (`invalid or revoked runner token`). Runners receiving HTTP 401 SHALL exit immediately with non-zero status.

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

### Requirement: A remote run finishes after its dependents are decided

Remote runs SHALL remain `running` until job completions resolve all dependent tasks into dispatched jobs or skipped results. Once no tasks remain pending or running, the run SHALL transition to its derived terminal status. Uncreatable jobs SHALL mark tasks `failed` to prevent hanging runs.

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

### Requirement: A remote run does not hang at start

When a remote run starts, Kestrix SHALL dispatch jobs for ready tasks or finish the run immediately. If no tasks are triggered, the run SHALL finish `passed` with zero jobs. Job creation failures SHALL mark the task or run `failed` with redacted error messages rather than hanging.

#### Scenario: A dependency outside the run does not hold a task back
- **WHEN** `deploy` (trigger `repo.app.changed`) depends on `lint` (trigger `repo.docs.changed`) and a run starts with only `app` changed
- **THEN** a job for `deploy` SHALL be available to a runner

#### Scenario: A dependency inside the run still holds a task back
- **WHEN** the same run starts with `app` and `docs` changed
- **THEN** a job for `lint` SHALL be available and none for `deploy`
- **AND** a job for `deploy` SHALL become available once `lint` passes

#### Scenario: Nothing is triggered
- **WHEN** a run starts and no task's trigger is satisfied
- **THEN** the run SHALL be `passed`, with no task result and no job

#### Scenario: The source archive cannot be built
- **WHEN** a run starts and its source archive cannot be built
- **THEN** the run SHALL be `failed` with an error naming the cause
- **AND** no job SHALL exist for it

#### Scenario: One job cannot be created
- **WHEN** a run starts, the job of one task cannot be created, and no other job exists
- **THEN** that task SHALL be recorded `failed` with an error naming the failure
- **AND** the run SHALL be `failed`

### Requirement: Manual tasks in a remote run

In a remote-mode run without `include_manual`, Kestrix SHALL record each triggered
manual task as `manual` before creating any job, and SHALL keep the run `running`
while tasks await approval. A manual task SHALL NOT be dispatched until approved.
If its dependency fails, is canceled, or is skipped, Kestrix SHALL record the task
as `skipped` with `dependency failed`. With `include_manual`, a manual task SHALL
be an ordinary task. Kestrix SHALL record the sources a remote run started from.

#### Scenario: A manual task waits for approval
- **WHEN** `deploy` is manual and depends on `build`, and a run starts without `include_manual`
- **THEN** `deploy` SHALL be `manual` from the moment the run starts
- **AND** after the runner reports `build` passed, the run SHALL still be `running` and no job SHALL exist for `deploy`

#### Scenario: Approval dispatches the waiting task
- **WHEN** an operator approves `deploy` in that run
- **THEN** a job for `deploy` SHALL be available to a runner
- **AND** the run SHALL stay `running`

#### Scenario: A failed dependency skips the gate
- **WHEN** the runner reports `build` failed in that run
- **THEN** `deploy` SHALL be recorded `skipped` with the error `dependency failed`
- **AND** the run SHALL become `failed`

#### Scenario: include_manual runs the manual task
- **WHEN** a run starts with `include_manual` and the runner reports `build` passed
- **THEN** a job for `deploy` SHALL be available without approval

#### Scenario: A run of manual tasks only waits
- **WHEN** every triggered task is manual and the run starts without `include_manual`
- **THEN** each of them SHALL be `manual`
- **AND** the run SHALL be `running`

#### Scenario: Approval can open the run's sources
- **WHEN** a remote run starts from a recorded primary source
- **THEN** the run SHALL record that source

### Requirement: Task artifacts reach dependent remote jobs

Runners executing tasks with artifacts SHALL upload declared files as an archive bounded by `KESTRIX_RUNNER_ARTIFACT_MAX_BYTES` (default 50 MiB) before completion. Dependent downstream jobs SHALL receive and unpack ancestor artifacts topologically over their workspaces. Stored artifacts SHALL be purged when runs reach terminal status.

#### Scenario: Deploy sees what build produced
- **WHEN** `build` declares `artifacts = ["dist/"]`, writes `dist/app.txt` and passes, and `deploy` depends on `build`
- **THEN** the job dispatched for `deploy` SHALL find `dist/app.txt` in its working directory
- **AND** this SHALL hold when the two jobs execute on different runners

#### Scenario: Oversized artifact
- **WHEN** a runner uploads an artifact archive larger than the configured limit
- **THEN** Kestrix SHALL respond `413` and store nothing
- **AND** the task SHALL be recorded as failed with an error naming the limit

#### Scenario: Stale claim cannot upload
- **WHEN** a runner uploads artifacts using a claim epoch that silent-runner recovery has superseded
- **THEN** the upload SHALL be rejected and SHALL NOT be stored

#### Scenario: Diamond order is deterministic
- **WHEN** `c` depends on `a` and `b`, the pipeline file declares `a` before `b`, and both wrote `out/x`
- **THEN** `c` SHALL see the `out/x` written by `b`

#### Scenario: Artifacts are deleted with the run
- **WHEN** a remote-mode run reaches a terminal status
- **THEN** its stored artifacts SHALL be deleted

### Requirement: Unmatched pending jobs are reported

Pending remote jobs requiring tags absent from all registered runners SHALL report a `waiting_reason` (`no registered runner has tags: <tags>` or `no runner is registered`). The reason SHALL be computed dynamically in run responses and displayed as a warning until a matching runner registers, without automatically failing the job.

#### Scenario: No runner has the required tag
- **WHEN** a remote-mode task with `runner_tags = ["gpu"]` is pending and the only registered runner has no tags
- **THEN** the run's response SHALL include `waiting_reason` `no registered runner has tags: gpu` for that task
- **AND** the run detail view SHALL show it as a warning

#### Scenario: No runner registered
- **WHEN** an untagged remote-mode task is pending and no runner is registered
- **THEN** its `waiting_reason` SHALL be `no runner is registered`

#### Scenario: Registering a matching runner clears the reason
- **WHEN** a runner tagged `gpu` is registered while that task is pending
- **THEN** the next response returning the run SHALL NOT include a `waiting_reason` for it

#### Scenario: An offline matching runner is not unmatched
- **WHEN** the only runner with the required tags is registered but offline
- **THEN** the task SHALL NOT carry a `waiting_reason`

#### Scenario: Revoked runners do not count
- **WHEN** the only runner with the required tags has been revoked
- **THEN** the task SHALL carry a `waiting_reason`

### Requirement: Remote trace append deduplication and event coordination

The system SHALL coordinate trace streaming from remote runners through the service layer, acknowledging idempotent trace retries without re-broadcasting duplicate chunks to real-time subscribers.

#### Scenario: Idempotent trace append retry is not re-broadcast
- **GIVEN** a remote runner has sent a trace chunk at offset `N`
- **WHEN** the runner retries sending the same chunk at offset `N`
- **THEN** the store SHALL acknowledge the retry successfully
- **AND** the event hub SHALL NOT broadcast the duplicate chunk to real-time subscribers

#### Scenario: Claimed remote task emits running transition
- **WHEN** a registered runner claims a pending remote job
- **THEN** the system SHALL broadcast a task event with status `running` for that task to real-time subscribers
