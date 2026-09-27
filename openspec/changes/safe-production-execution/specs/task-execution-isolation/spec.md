## ADDED Requirements

### Requirement: Local execution requires an explicit opt-in

Kestrix SHALL execute a task in `local` mode inside its own process only when
the instance setting `runner.allow_local` (environment variable
`KESTRIX_RUNNER_ALLOW_LOCAL`) is true. The setting SHALL default to false and
follow the configuration precedence rule. When it is false:
- startup SHALL fail if the instance runner mode is `local` or unset, with a
  message stating that `local` tasks can read Kestrix's key files and naming
  `remote` and the opt-in as the alternatives;
- creating or updating a project with mode `local` SHALL be rejected as a
  validation error;
- a run of a stored project whose mode is `local` SHALL fail before any
  source is acquired or any task executes, with the same explanation.

When it is true, startup SHALL log a warning naming the exposure and the
number of stored projects in `local` mode.

#### Scenario: Local instance mode without the opt-in
- **WHEN** Kestrix starts with `runner.mode` unset and `KESTRIX_RUNNER_ALLOW_LOCAL` unset
- **THEN** startup SHALL fail with a message naming the key-file exposure, `remote`, and `KESTRIX_RUNNER_ALLOW_LOCAL`

#### Scenario: Local project rejected without the opt-in
- **WHEN** the opt-in is off and a client creates a project with mode `local`
- **THEN** the request SHALL be rejected as a validation error

#### Scenario: Stored local project does not execute
- **WHEN** the opt-in is off and a run of a stored `local`-mode project is started
- **THEN** the run SHALL fail with the explanation
- **AND** no task script SHALL execute

#### Scenario: Opt-in accepted for a single-user machine
- **WHEN** Kestrix starts with `runner.mode: local` and `KESTRIX_RUNNER_ALLOW_LOCAL=true`
- **THEN** startup SHALL succeed and log a warning naming the exposure
