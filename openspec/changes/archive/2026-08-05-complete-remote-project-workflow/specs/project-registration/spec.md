## MODIFIED Requirements

### Requirement: REQ-1: Project config includes GitLab binding (implemented)

Each registered project SHALL include `name`, non-empty `repo_url`, non-empty
`repo_ref`, relative `pipeline_dir`, and `created_at`. It MAY include
`repo_credential_ref`, `pipeline_file`, and the existing optional GitLab
binding, default-branch, and webhook fields. Registered projects SHALL NOT use
or expose `repo_path`, and project definitions SHALL contain credential
references only, never credential values.

#### Scenario: Register a public remote project

- **WHEN** a project is created with `name`, `repo_url`, `repo_ref`, and a
  relative pipeline directory
- **THEN** Kestrix SHALL persist the remote source declaration
- **AND** SHALL not require a host-local checkout path

#### Scenario: Register a private remote project

- **WHEN** a project is created with an active `repo_credential_ref`
- **THEN** Kestrix SHALL persist only that stable reference
- **AND** project responses SHALL not contain credential values

#### Scenario: Reject local project registration

- **WHEN** project create or update input contains `repo_path` or omits either
  `repo_url` or `repo_ref`
- **THEN** validation SHALL reject the input before persistence
- **AND** SHALL identify the required remote fields

### Requirement: REQ-5: API CRUD for projects (implemented)

Project CRUD endpoints SHALL accept and expose non-secret remote source
metadata, including `repo_url`, `repo_ref`, optional `repo_credential_ref`, and
the relative pipeline location. Create and source-changing update operations
SHALL validate source access and parse the pipeline before atomically
persisting the configuration. Responses SHALL omit `repo_path`, credential
values, and cache filesystem paths.

#### Scenario: Create validates source and pipeline

- **WHEN** `POST /api/projects` receives a complete remote project payload
- **THEN** Kestrix SHALL acquire the requested ref and parse its pipeline files
- **AND** SHALL create the row only when acquisition and parsing succeed

#### Scenario: Invalid source update preserves the old project

- **WHEN** `PUT /api/projects/{name}` changes the URL, ref, credential reference,
  or pipeline location to an invalid value
- **THEN** the endpoint SHALL return a redacted validation error
- **AND** the previously persisted configuration SHALL remain unchanged

#### Scenario: Clear an optional credential

- **WHEN** an update explicitly clears `repo_credential_ref` for a public source
- **THEN** Kestrix SHALL persist the source without a credential reference
- **AND** SHALL not restore the previous reference through merge defaults

## ADDED Requirements

### Requirement: Project onboarding UI matches remote registration

The project create and edit UI SHALL collect repository URL, required ref,
optional active credential reference, and relative pipeline directory. It SHALL
not request a local repository path and SHALL present field-specific,
secret-redacted validation failures.

#### Scenario: Create project from the UI

- **WHEN** a user opens the new-project dialog
- **THEN** the form SHALL require project name, repository URL, and ref
- **AND** SHALL offer only non-revoked credential metadata without exposing
  credential values

#### Scenario: Incomplete form

- **WHEN** URL or ref is empty
- **THEN** the create action SHALL remain unavailable or show an inline required
  field error
- **AND** the UI SHALL not submit `repo_path`

#### Scenario: First private project needs a new credential

- **WHEN** a fresh installation has no suitable active credential
- **THEN** project onboarding SHALL allow the user to create one through the
  write-only credential operation without first selecting an existing project
- **AND** SHALL refresh and select the returned credential metadata reference

### Requirement: Pipeline repositories remain pipeline-owned

Additional repositories SHALL be defined by parsed `ci_repo` blocks rather than
a separate project-registration structure. Project detail SHALL display those
repositories as read-only source metadata and SHALL not offer a project form
whose values are not persisted to the pipeline definition.

#### Scenario: View additional repositories

- **WHEN** a project's parsed pipeline declares local subpaths or additional
  remote repositories
- **THEN** project detail SHALL display their names and non-secret source fields
- **AND** changes SHALL require a commit to the remote pipeline definition

### Requirement: Legacy local projects require explicit migration

The remote-only database migration SHALL detect registered projects that have
no valid URL and ref and SHALL stop with actionable project identifiers before
removing the local-path schema. It SHALL NOT reinterpret a filesystem path as a
Git URL or silently retain an unusable row.

#### Scenario: Upgrade with a local-only project

- **WHEN** migration preflight finds a project containing only `repo_path`
- **THEN** startup SHALL fail before destructive schema changes
- **AND** diagnostics SHALL instruct the operator to replace or delete that
  project explicitly
