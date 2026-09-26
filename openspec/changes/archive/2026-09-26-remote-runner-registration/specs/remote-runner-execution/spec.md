## Purpose

Lets a task execute on a machine other than the Kestrix host: a runner
registers with Kestrix, polls for work, executes with its own locally
configured engine, and streams output and a result back.

## ADDED Requirements

### Requirement: Runner registration

An administrator SHALL be able to register a runner by name and receive a
bearer token, shown once. The token SHALL be stored only in hashed form. A
runner SHALL present this token on every call to the runner-job endpoints. An
administrator SHALL be able to revoke a runner, immediately invalidating its
token.

#### Scenario: Token shown once
- **WHEN** an administrator registers a new runner
- **THEN** the response SHALL include the token
- **AND** no later API response SHALL ever return that token again

#### Scenario: Revocation is immediate
- **WHEN** an administrator revokes a runner
- **THEN** a subsequent call using that runner's token SHALL be rejected with `401`

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
applied to every other execution mode before it is stored. A trace append
SHALL present the claim epoch it was issued; Kestrix SHALL reject the append
without storing it if the job's current claim epoch no longer matches.
Appending SHALL be safe against concurrent appends: when two jobs of the same
run execute on different runners at once, neither runner's accepted output
SHALL be lost or overwritten by the other's append. Relative ordering between
two jobs' chunks is not guaranteed; not losing them is. Appending SHALL
enforce the same `max_output_bytes` bound as every other mode: once a run's
stored output reaches the bound, further appended content SHALL be dropped
and the stored output SHALL end with the truncation marker, without changing
the run's or any task's status. Redaction SHALL apply before truncation.

#### Scenario: Live output visible during execution
- **WHEN** a runner appends output to a running job's trace
- **THEN** the run's stored output SHALL reflect it before the job completes

#### Scenario: Redaction applies to remote output
- **WHEN** appended output contains the plaintext value of a resolved secret
- **THEN** the stored output SHALL contain the redaction placeholder, not the value

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

A runner SHALL report a claimed job's outcome exactly once: a terminal status
and any remaining output. Kestrix SHALL record the task's result and resume
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
to claim. A job returned to `pending` a configured number of times without
completing SHALL fail with an error naming the reason. Recovery SHALL NOT
return a job to `pending` when its run is already in a terminal status: such
a job SHALL be finalized in place, so cancelling a run is never undone by a
later reclaim.

#### Scenario: Job reclaimed after timeout
- **WHEN** a claimed job receives no update for longer than the configured timeout
- **THEN** it SHALL become `pending` again and be eligible for another runner

#### Scenario: Repeated timeout fails the job
- **WHEN** a job has been reclaimed the configured maximum number of times
- **THEN** it SHALL be marked failed with a message naming that it was abandoned by unresponsive runners

#### Scenario: A canceled run's abandoned job is not reclaimed
- **WHEN** a job goes silent past the timeout and its run has already been marked `canceled`
- **THEN** the job SHALL NOT return to `pending`
- **AND** no runner SHALL receive it on a subsequent poll

### Requirement: Runner liveness tracking (Phase 2)

Kestrix SHALL record the time of a runner's most recent poll and SHALL surface
whether a runner is online or offline based on a configured staleness
threshold.

#### Scenario: Runner goes offline
- **WHEN** a runner has not polled within the staleness threshold
- **THEN** it SHALL be reported offline

### Requirement: Runner tags and task matching (Phase 2)

A runner SHALL be able to register with a set of tags. A task SHALL be able to
declare tags it requires. A job SHALL only be offered to a runner whose tags
are a superset of the task's required tags.

#### Scenario: Job withheld from an unmatched runner
- **WHEN** a task requires a tag no polling runner currently has registered
- **THEN** the job SHALL remain `pending` rather than being offered to a non-matching runner

### Requirement: Runner management interface (Phase 2)

Kestrix SHALL provide a Settings page listing registered runners, their
liveness, and an action to revoke each one.

#### Scenario: Revoke from the UI
- **WHEN** an administrator revokes a runner from the Settings page
- **THEN** the runner's token SHALL be invalidated the same as an API-driven revocation
