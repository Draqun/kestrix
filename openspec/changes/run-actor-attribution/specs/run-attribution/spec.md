## Purpose

Run attribution records which actor started, approved, or canceled a pipeline
run, so that every human-initiated change to a run's state can be traced back
to an account after the fact, even once that account no longer exists.

## ADDED Requirements

### Requirement: Actor representation

An actor SHALL be represented as one opaque string of the form `<kind>:<id>`,
where `<kind>` is a lowercase label and `<id>` is the identifier meaningful for
that kind. An action performed through an authenticated API request SHALL be
attributed to `user:<account name>`, using the account name resolved by
authentication for that request and never a caller-supplied value. Kestrix
SHALL NOT produce any other kind in this change; `webhook:` and `cli:` are
reserved for run origins that do not yet create run records.

#### Scenario: Authenticated start is attributed to the account

- **WHEN** an account named `alice` starts a run through the API
- **THEN** the run's `started_by` SHALL be `user:alice`

#### Scenario: Caller-supplied actor is ignored

- **WHEN** a request to start, approve, or cancel a run carries an actor name in its body, a header, or a query parameter
- **THEN** Kestrix SHALL attribute the action to the authenticated account only

### Requirement: Run start is attributed

Kestrix SHALL record the actor that started a run at the moment the run record
is created, and SHALL expose it as `started_by` on every response that returns
the run. A run created before attribution existed SHALL expose an empty
`started_by`.

#### Scenario: Run detail carries the starter

- **WHEN** a client reads a run that was started by an authenticated account
- **THEN** the response SHALL include `started_by` equal to that account's actor string

#### Scenario: Run list carries the starter

- **WHEN** a client lists runs for a project or across projects
- **THEN** every listed run SHALL include its `started_by`

#### Scenario: Pre-existing run has no starter

- **WHEN** a client reads a run recorded before this requirement took effect
- **THEN** the response SHALL include `started_by` as an empty string
- **AND** SHALL NOT fail or omit the run

### Requirement: Manual approval is attributed

When an actor approves a manual task, Kestrix SHALL record `approved_by` and
`approved_at` on that task's result entry, and SHALL expose both on every
response that returns the run's task results. A task never approved SHALL carry
neither field.

#### Scenario: Approved task names the approver

- **WHEN** an account approves a manual task that is waiting for approval
- **THEN** the task's latest result entry SHALL carry `approved_by` equal to that account's actor string
- **AND** SHALL carry an `approved_at` timestamp no earlier than the approval request

#### Scenario: Approver survives the task's outcome

- **WHEN** an approved manual task later finishes as `passed` or `failed`
- **THEN** the task's latest result entry SHALL still carry the same `approved_by` and `approved_at`

#### Scenario: Unapproved task carries no approver

- **WHEN** a client reads a run whose manual task is still waiting for approval
- **THEN** that task's result entry SHALL NOT include `approved_by` or `approved_at`

#### Scenario: Refused approval records nothing

- **WHEN** an approval request is rejected because the task is not waiting for approval or its dependencies have not passed
- **THEN** the task's result entries SHALL be unchanged

### Requirement: Cancellation is attributed

When an actor cancels a running run, Kestrix SHALL record `canceled_by` on the
run and SHALL expose it on every response that returns the run. A run that was
not canceled SHALL carry an empty `canceled_by`. A cancel request against a run
that is not running SHALL leave `canceled_by` unchanged.

#### Scenario: Canceled run names the canceler

- **WHEN** an account cancels a run whose status is `running`
- **THEN** the run's status SHALL become `canceled`
- **AND** its `canceled_by` SHALL be that account's actor string

#### Scenario: Cancel of a finished run records nothing

- **WHEN** an account cancels a run whose status is already `passed`, `failed`, or `canceled`
- **THEN** the run's `canceled_by` SHALL be unchanged

### Requirement: Attribution outlives the account

Attribution SHALL be stored as the actor string itself, with no reference that
requires the account to still exist. Deleting an account SHALL NOT alter,
clear, or hide the attribution on runs it started, approved, or canceled.

#### Scenario: Deleted account remains in history

- **WHEN** an account that started a run is deleted
- **THEN** reading that run SHALL still return the original `started_by`

### Requirement: Attribution is shown in the interface

The run detail view SHALL show who started the run next to its start time, who
canceled it when it was canceled, and who approved each approved manual task.
The runs list SHALL show who started each run. An empty actor SHALL be shown as
`unknown`.

#### Scenario: Run detail shows the actors

- **WHEN** a user opens the detail of a run that was started, had a task approved, and was then canceled
- **THEN** the view SHALL show the starter, the approver on that task, and the canceler

#### Scenario: Legacy run shows unknown starter

- **WHEN** a user opens the detail of a run whose `started_by` is empty
- **THEN** the view SHALL show `unknown` as the starter

### Requirement: Attribution is logged

Kestrix SHALL write one log record for each run start, manual approval, and
cancellation, carrying the actor, the project name, the run id, and for an
approval the task name. The record SHALL NOT contain the bearer token, the
password, or any credential value.

#### Scenario: Start is logged

- **WHEN** an account starts a run
- **THEN** Kestrix SHALL log a record naming the actor, project, and run id

#### Scenario: Log carries no secret

- **WHEN** any attribution record is written
- **THEN** it SHALL NOT contain the request's bearer token or any substring of it
