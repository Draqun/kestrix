# Repository Credentials Specification

## Purpose

Define encrypted, provider-neutral credentials for remote Git source acquisition.

## Requirements

### Requirement: Encrypted provider-neutral credential storage

Kestrix SHALL store Git credentials and run-time `secret` credentials in PostgreSQL as authenticated encrypted data referenced by a stable `credential_ref`. The encryption master key SHALL remain outside PostgreSQL and SHALL be supplied only by the validated file path configured through `KESTRIX_MASTER_KEY_PATH`. Plaintext credential values SHALL NOT be stored in pipeline definitions, run metadata, logs, command arguments, or API responses. Kestrix SHALL NOT use an OS keyring or other implicit fallback to obtain the encryption master key. The credential type SHALL be bound to the ciphertext so that a stored record cannot be reinterpreted as a different type.

#### Scenario: Store a GitHub or GitLab token

- **WHEN** an operator creates a token credential for a GitHub or GitLab host
- **THEN** Kestrix SHALL normalize and encrypt the token before inserting it into PostgreSQL
- **AND** subsequent metadata responses SHALL omit the token value

#### Scenario: Store a Bitbucket app password

- **WHEN** an operator creates a Bitbucket app-password credential
- **THEN** Kestrix SHALL store it using the provider-neutral username/password representation
- **AND** the database row SHALL contain ciphertext rather than either secret value

#### Scenario: Store a run-time secret

- **WHEN** an administrator creates a credential of type `secret`
- **THEN** Kestrix SHALL encrypt its value under the same master key and store it in the same table
- **AND** metadata responses SHALL report type `secret` and omit the value

### Requirement: Credential lifecycle API

The system SHALL provide create, rotate, inspect-metadata, and revoke operations for credentials. Secret-bearing operations SHALL be write-only; metadata responses SHALL include the reference, type, host/provider metadata, creation/update timestamps, and revocation state but SHALL NOT include secrets.

#### Scenario: Rotate a credential

- **WHEN** an operator submits a replacement secret for an existing `credential_ref`
- **THEN** Kestrix SHALL atomically replace the encrypted payload
- **AND** future acquisitions SHALL use the replacement without changing the pipeline definition

#### Scenario: Revoke a credential

- **WHEN** an operator revokes a credential reference
- **THEN** future source acquisitions using that reference SHALL fail before task execution
- **AND** the response SHALL not reveal the revoked secret

### Requirement: Secure runtime resolution

The source-acquisition port SHALL resolve Git credentials by reference at runtime and SHALL expose plaintext values only to the Git child process. Temporary credential files SHALL use restrictive permissions and SHALL be removed after success, failure, cancellation, or timeout. Git credential types (`token`, `basic`, `ssh`) SHALL NOT be injected into task processes or deployment environments; only a credential of type `secret`, referenced from a pipeline variable, reaches a task, and only as specified by the `runtime-secrets` capability. The source-acquisition port SHALL refuse a credential of type `secret`.

#### Scenario: HTTPS token acquisition

- **WHEN** a remote source uses a valid token credential reference
- **THEN** Kestrix SHALL authenticate Git without placing the token in the URL or command arguments
- **AND** the token SHALL be unavailable after the Git process exits

#### Scenario: SSH acquisition cleanup

- **WHEN** a remote source uses an SSH private-key credential and checkout completes or fails
- **THEN** Kestrix SHALL remove temporary key and known-hosts material
- **AND** Git diagnostics SHALL be redacted before being returned or logged

#### Scenario: Git credential never reaches a task

- **WHEN** a task runs in a project whose sources were acquired with a Git credential
- **THEN** the task process environment SHALL NOT contain that credential's token, password, or private key

#### Scenario: Secret credential refused for Git

- **WHEN** source acquisition resolves a `credential_ref` whose credential type is `secret`
- **THEN** acquisition SHALL fail before any Git process starts
- **AND** the error SHALL name the type mismatch and SHALL NOT contain the value

#### Scenario: Secret credential refused earlier, at project create or update

- **WHEN** a client creates or updates a project whose `repo_credential_ref`, or whose pipeline declares a `ci_repo` block with a `credential_ref`, resolves to a credential of type `secret`
- **THEN** Kestrix SHALL reject the request with a validation error before the project is persisted
- **AND** SHALL name the offending `ci_repo`, when it is a secondary one, rather than only failing later when source acquisition runs

### Requirement: Support standard Git credential forms

The credential model SHALL support HTTPS token, HTTPS username/password or app password, SSH private key, and optional SSH known-hosts data without requiring provider-specific API clients. The model SHALL additionally support a `secret` form holding one opaque value for run-time use; `provider` and `host` metadata are optional for it and SHALL remain descriptive only.

#### Scenario: Generic Git host

- **WHEN** a source points to a self-hosted or arbitrary Git server
- **THEN** Kestrix SHALL use the same credential resolver and standard Git transport as for GitHub, GitLab, and Bitbucket

#### Scenario: Secret form has one value field

- **WHEN** a `secret` credential is created with `username`, `password`, `token`, or `private_key` fields
- **THEN** Kestrix SHALL reject the request as an invalid `secret` credential
