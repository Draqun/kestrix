## MODIFIED Requirements

### Requirement: REQ-1: Project config includes GitLab binding (implemented)

Each project configuration SHALL include its existing GitLab binding fields and
MAY include a provider-neutral `repo_url`, `repo_ref`, and
`repo_credential_ref` for a remote source. A remote project SHALL provide both
`repo_url` and a non-empty `repo_ref`; local projects SHALL continue to use
`repo_path`. Credential values SHALL NOT be accepted in project definitions.

#### Scenario: Register a remote project

- **WHEN** a project is created with `name`, `repo_url`, `repo_ref`, and an
  optional `repo_credential_ref`
- **THEN** Kestrix SHALL persist the source declaration and credential
  reference
- **AND** it SHALL not persist any credential value

#### Scenario: Reject an incomplete remote project

- **WHEN** a project has `repo_url` without `repo_ref`
- **THEN** project validation SHALL fail before persistence
- **AND** the response SHALL explain that the remote ref is required

### Requirement: REQ-5: API CRUD for projects (implemented)

Existing project CRUD endpoints SHALL accept and expose non-secret remote
source metadata, including `repo_url`, `repo_ref`, and
`repo_credential_ref`. Credential values SHALL be write-only through dedicated
credential operations and SHALL never appear in project or run responses.

#### Scenario: Update a remote source reference

- **WHEN** a client updates `repo_ref` through project CRUD
- **THEN** the new requested ref SHALL be persisted
- **AND** future runs SHALL use the new ref without exposing credential data
