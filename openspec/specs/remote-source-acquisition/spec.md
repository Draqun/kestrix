# Remote Source Acquisition Specification

## Purpose

Define secure, provider-neutral acquisition of remote Git source code for pipeline runs.

## Requirements

### Requirement: Acquire remote Git sources before execution

The system SHALL acquire every remote repository into an isolated per-run workspace before starting any pipeline task. Acquisition SHALL use standard Git transport and SHALL NOT require a provider-specific API.

#### Scenario: Public repository on an arbitrary Git host

- **WHEN** a repository declaration contains a public HTTPS or SSH Git URL
- **THEN** Kestrix SHALL clone it into the run workspace
- **AND** SHALL expose the acquired directory to tasks using the repository's logical name
- **AND** SHALL not require GitHub, GitLab, or Bitbucket-specific configuration

#### Scenario: Private repository with a credential reference

- **WHEN** a repository declaration contains a private Git URL and a configured `credential_ref`
- **THEN** Kestrix SHALL resolve the credential for the acquisition process
- **AND** SHALL clone the repository without persisting the credential value

### Requirement: Resolve a deterministic source revision

The system SHALL support a branch, tag, or commit as the requested revision. Before starting tasks, it SHALL resolve the checked-out source to an immutable commit SHA and SHALL record the requested revision and resolved SHA in the run context.

#### Scenario: Branch resolves successfully

- **WHEN** a remote repository requests an existing branch
- **THEN** Kestrix SHALL check out that branch's current commit
- **AND** SHALL record the resulting commit SHA

#### Scenario: Revision does not exist

- **WHEN** the requested branch, tag, or commit cannot be resolved
- **THEN** acquisition SHALL fail with an actionable error
- **AND** no pipeline task SHALL start

### Requirement: Isolate and clean source workspaces

The system SHALL use a separate workspace for each run and SHALL clean it after successful or unsuccessful completion, cancellation, or timeout unless explicit debug retention is enabled.

#### Scenario: Concurrent runs

- **WHEN** two runs acquire the same remote repository concurrently
- **THEN** each run SHALL receive a distinct workspace
- **AND** files or checkout state from one run SHALL not be visible to the other

#### Scenario: Acquisition timeout

- **WHEN** cloning or checkout exceeds its configured timeout
- **THEN** acquisition SHALL stop or terminate the Git process
- **AND** SHALL clean the partial workspace
- **AND** SHALL report the timeout without exposing credentials

### Requirement: Handle credentials securely

The system SHALL support credential references resolved through configured environment-backed credentials, Git credential helpers, or an SSH agent. Secrets SHALL be process-scoped and SHALL NOT appear in pipeline definitions, URLs, run metadata, command arguments, or logs.

#### Scenario: Missing credential

- **WHEN** a private source requires a credential that cannot be resolved
- **THEN** acquisition SHALL fail before task execution
- **AND** SHALL identify the missing reference without printing its value

#### Scenario: Embedded credentials in URL

- **WHEN** a source URL contains a username/password or token
- **THEN** validation SHALL reject the declaration
- **AND** error output SHALL redact any sensitive URL components
