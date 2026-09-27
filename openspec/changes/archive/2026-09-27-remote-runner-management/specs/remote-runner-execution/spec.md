## RENAMED Requirements

- FROM: `### Requirement: Runner liveness tracking (Phase 2)`
- TO: `### Requirement: Runner liveness tracking`

- FROM: `### Requirement: Runner tags and task matching (Phase 2)`
- TO: `### Requirement: Runner tags and task matching`

- FROM: `### Requirement: Runner management interface (Phase 2)`
- TO: `### Requirement: Runner management interface`

## MODIFIED Requirements

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
