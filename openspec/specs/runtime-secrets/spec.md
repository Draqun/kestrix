# runtime-secrets Specification

## Purpose
Lets a pipeline reference encrypted run-time secrets by name so tasks can
deploy without any secret value ever appearing in Git, the database in
plaintext, run output, or an API response.

## Requirements

### Requirement: Secret credential type

The credential store SHALL accept a credential of type `secret` holding one
opaque value and a display name. A `secret` credential SHALL be created,
rotated, inspected, listed, and revoked through the same credential lifecycle
operations, with the same administrator-only authorization, write-only
semantics, and encryption under the master key as Git credentials. Metadata
responses for a `secret` credential SHALL NOT contain the value, its length,
or any derivative of it.

#### Scenario: Create a secret

- **WHEN** an administrator creates a credential with `type = "secret"` and a non-empty value
- **THEN** Kestrix SHALL encrypt the value before storing it
- **AND** SHALL return metadata carrying the reference, name, and type only

#### Scenario: Empty value is rejected

- **WHEN** a `secret` credential is created or rotated with an empty value
- **THEN** Kestrix SHALL reject the request with `400`
- **AND** SHALL leave any existing credential under that reference unchanged

#### Scenario: Non-administrator is refused

- **WHEN** an account whose effective role is below `admin` creates, rotates, or revokes a `secret` credential
- **THEN** Kestrix SHALL reject the request with `403`

### Requirement: Secret reference in a variable

A `variable` block MAY set `secret_ref` to the reference of a `secret`
credential. A variable with `secret_ref` SHALL NOT also set `value` or
`default`, and SHALL be treated as masked regardless of the `masked`
attribute. `scope`, `repo`, `job`, `env`, and `protected` SHALL apply to a
secret variable exactly as to a plaintext one. A parsed definition, including
the pipeline snapshot stored with a run and every API response that returns
it, SHALL carry the reference and SHALL NOT carry the value.

#### Scenario: Secret variable is accepted

- **WHEN** a pipeline declares `variable "REGISTRY_PASSWORD" { secret_ref = "cred_abc" scope = "job" repo = "app" job = "publish" }`
- **THEN** validation SHALL succeed
- **AND** the parsed variable SHALL report the reference, the job scope, and masked state

#### Scenario: Secret reference and literal value together

- **WHEN** a `variable` block sets `secret_ref` together with `value` or `default`
- **THEN** validation SHALL fail naming the variable and the conflicting attributes
- **AND** Kestrix SHALL not start the pipeline

#### Scenario: Snapshot carries the reference only

- **WHEN** a run is created from a definition containing a secret variable
- **THEN** the stored pipeline snapshot and `GET /api/projects/{name}/runs/{id}` SHALL show the variable with its `secret_ref`
- **AND** SHALL NOT contain the resolved value

### Requirement: Secrets are resolved at run start and injected as variables

Kestrix SHALL resolve every secret variable a run may use when the run
starts, and when an approved manual task starts, and SHALL NOT resolve at any
other time. A resolved value SHALL reach a task only as an environment
variable under the variable's key, subject to the variable's scope, and SHALL
be treated as a pipeline variable by any environment allowlist the runner
applies. Kestrix SHALL NOT pass resolved values to a dispatched GitLab
pipeline.

#### Scenario: Task receives the value

- **WHEN** a task in a matching scope runs after its secret variable resolved
- **THEN** the task process environment SHALL contain the variable key set to the decrypted value
- **AND** tasks outside that scope SHALL NOT receive it

#### Scenario: Manual approval resolves fresh

- **WHEN** a secret is rotated after a run starts and before its manual task is approved
- **THEN** the approved task SHALL receive the rotated value

#### Scenario: Dispatch never carries a secret

- **WHEN** a webhook dispatches a GitLab pipeline for a definition with secret variables
- **THEN** the dispatched pipeline variables SHALL contain no secret variable key or value

### Requirement: Unresolvable secret fails the run before execution

When a secret variable's reference does not exist, is revoked, names a
credential whose type is not `secret`, or cannot be decrypted, Kestrix SHALL
fail the run before any task starts. The run error SHALL name the variable key
and the failure kind and SHALL NOT contain the reference's value, the
credential name, or the decryption error detail. Every task of the run SHALL
be recorded as `skipped`.

#### Scenario: Revoked secret

- **WHEN** a run starts and one of its secret variables references a revoked credential
- **THEN** the run SHALL finish `failed` with an error naming that variable key
- **AND** no task SHALL have started
- **AND** every task SHALL be recorded as `skipped`

#### Scenario: Git credential used as a secret

- **WHEN** a secret variable references a credential of type `token`, `basic`, or `ssh`
- **THEN** the run SHALL fail before any task starts
- **AND** the error SHALL state that the reference is not a `secret` credential
- **AND** SHALL NOT contain any field of that credential

#### Scenario: Error is not an oracle for the value

- **WHEN** a secret fails to decrypt
- **THEN** the run error SHALL identify the variable and that resolution failed
- **AND** SHALL NOT contain the ciphertext, plaintext, or driver error text

### Requirement: Secret values are redacted from persisted and returned output

Kestrix SHALL replace every occurrence of a resolved secret value of at least
8 bytes with `***` in run output and task output before that output is
persisted, flushed, or returned by any API. Redaction SHALL apply to output
produced by every task of the run, not only the task whose scope holds the
variable. Kestrix SHALL NOT log a resolved value. A resolved value shorter
than 8 bytes SHALL still be injected but is not guaranteed to be redacted, and
the documentation SHALL say so.

#### Scenario: Echoed secret is redacted

- **WHEN** a task writes a resolved secret value to standard output or standard error
- **THEN** the run output stored for the run and returned by the run API SHALL contain `***` in its place
- **AND** the task result output SHALL contain `***` in its place

#### Scenario: Redaction covers partial flushes

- **WHEN** run output is flushed to storage while the run is still executing
- **THEN** the flushed text SHALL already be redacted

#### Scenario: Secret is not written to the log

- **WHEN** a run with secret variables starts, fails, or completes
- **THEN** no server log record SHALL contain a resolved value

### Requirement: Secret credentials are isolated from repository credentials

A `secret` credential SHALL NOT be accepted as a project's repository
credential or as a `ci_repo` `credential_ref`, and the source-acquisition path
SHALL refuse to use one. The project credential selector in the interface
SHALL NOT offer `secret` credentials.

#### Scenario: Secret rejected for source acquisition

- **WHEN** a project is created or updated with a `repo_credential_ref` naming a `secret` credential
- **THEN** Kestrix SHALL reject the request with `400`
- **AND** SHALL state that the credential type is not usable for Git access

#### Scenario: Selector hides secrets

- **WHEN** an administrator opens the project create or edit dialog
- **THEN** the credential selector SHALL list Git credentials only
