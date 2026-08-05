## MODIFIED Requirements

### Requirement: Acquire remote Git sources before execution

The system SHALL acquire a registered project's primary remote repository and
each additional remote `ci_repo` through one source-session service before any
consumer parses or executes its content. Project validation, definition reads,
trigger resolution, webhooks, full runs, single tasks, and manual approvals
SHALL use this boundary and SHALL NOT depend on a registered host-local path.
Standard Git transport SHALL remain provider-neutral.

#### Scenario: Read a project definition

- **WHEN** an API consumer requests a registered project's raw or parsed
  pipeline definition
- **THEN** Kestrix SHALL open an isolated source session at the registered ref
- **AND** SHALL clean the session after producing the response

#### Scenario: Execute a project with additional remote repositories

- **WHEN** a run's pipeline contains additional remote `ci_repo` declarations
- **THEN** Kestrix SHALL acquire all required sources before starting any task
- **AND** SHALL expose each checkout by its logical repository name

### Requirement: Resolve a deterministic source revision

The system SHALL resolve every requested branch, tag, commit, or webhook
checkout revision to an immutable commit SHA before consuming source content.
It SHALL record the primary and additional repositories' non-secret source
identities, requested revisions, credential references, and resolved SHAs in
run context and use that recorded identity when an existing run executes later
work. It SHALL NOT persist source-session or cache paths.

#### Scenario: Branch resolves successfully

- **WHEN** a new interactive run requests an existing branch
- **THEN** Kestrix SHALL refresh and resolve the branch's current commit
- **AND** SHALL record and execute that immutable SHA

#### Scenario: Webhook identifies an event commit

- **WHEN** a valid push webhook supplies an immutable checkout SHA
- **THEN** Kestrix SHALL parse and resolve the pipeline from that SHA
- **AND** SHALL not substitute the current branch head

#### Scenario: Manual task is approved after the branch moves

- **WHEN** a manual task is approved for a run whose requested branch now points
  to another commit
- **THEN** Kestrix SHALL reacquire every source from that run's recorded SHA
- **AND** SHALL execute the stored pipeline snapshot rather than the live project
  definition

#### Scenario: Revision does not exist

- **WHEN** the exact requested or recorded revision cannot be materialized
- **THEN** acquisition SHALL fail with an actionable redacted error
- **AND** no task SHALL execute from a substitute revision

### Requirement: Isolate and clean source workspaces

The system SHALL use a separate private source session for each project
validation, definition read, webhook, run, or task operation. It SHALL clean
session workspaces after success, failure, cancellation, or timeout unless an
explicit debug-retention option is enabled for that operation.

#### Scenario: Concurrent consumers

- **WHEN** a definition request and a run acquire the same source concurrently
- **THEN** each operation SHALL receive a distinct writable checkout
- **AND** files or checkout state from one SHALL not be visible to the other

#### Scenario: Acquisition timeout

- **WHEN** refresh, ref resolution, or checkout exceeds its configured timeout
- **THEN** Kestrix SHALL terminate the Git operation
- **AND** SHALL clean partial session material and preserve cache integrity

### Requirement: Handle credentials securely

The system SHALL resolve optional credential references from the configured
encrypted credential store for every acquisition, including cache hits.
Secrets SHALL be process-scoped and SHALL NOT appear in pipeline definitions,
URLs, cache identity or metadata, run metadata, command arguments, or logs.

#### Scenario: Private cached source credential is missing or revoked

- **WHEN** a private source references a credential that cannot currently be
  resolved
- **THEN** acquisition SHALL fail before cached content is materialized
- **AND** SHALL identify only the missing or revoked reference

#### Scenario: Embedded credentials in URL

- **WHEN** a source URL contains a password, token, or unsupported userinfo
- **THEN** validation SHALL reject the declaration
- **AND** error output SHALL redact sensitive URL components

## ADDED Requirements

### Requirement: Preserve sources needed by executable run state

Kestrix SHALL pin cached commits needed by a run while pending or manual tasks
can still execute. Cache maintenance SHALL not prune or evict those commits,
and Kestrix SHALL release the pins when no task in the run can execute again.

#### Scenario: Cache maintenance runs before manual approval

- **WHEN** a run is waiting for manual approval and normal cache retention runs
- **THEN** every recorded source commit required by that run SHALL remain pinned
- **AND** approval SHALL be able to materialize the exact recorded sources

#### Scenario: Run becomes terminal

- **WHEN** no pending or manual task in a run can execute again
- **THEN** Kestrix SHALL release its source pins
- **AND** normal cache retention MAY later evict the unpinned entries
