## MODIFIED Requirements

### Requirement: Repository Declaration (implemented)

A project SHALL declare one or more `ci_repo` blocks. A repository inside the
registered project's primary checkout SHALL define exactly one clean relative
`path`; it SHALL NOT use an absolute path or escape the acquired checkout. An
additional remote repository SHALL define exactly one `url`, a non-empty `ref`
identifying a branch, tag, or commit, and MAY define `credential_ref`. A block
SHALL NOT combine `path` and `url`, and credential values SHALL NOT be embedded
in URLs or stored in HCL.

#### Scenario: Declare a monorepo subpath

- **WHEN** a user writes `ci_repo "auth" { path = "src/auth" }`
- **THEN** the system SHALL resolve `src/auth` beneath the acquired primary
  project checkout
- **AND** SHALL reject traversal outside that checkout

#### Scenario: Declare an additional remote repository

- **WHEN** a user writes a `ci_repo` with URL, ref, and optional credential
  reference
- **THEN** Kestrix SHALL acquire that source through the source-session service
- **AND** SHALL record its resolved commit SHA for the run

#### Scenario: Reject an ambiguous source

- **WHEN** a `ci_repo` contains both `path` and `url`, an absolute path, or a URL
  without a non-empty ref
- **THEN** pipeline validation SHALL fail before any task starts

## ADDED Requirements

### Requirement: Remote Git owns registered pipeline content

Pipeline files loaded for a registered project SHALL be read from its acquired
remote revision. Kestrix SHALL expose raw and parsed definitions for viewing
but SHALL NOT modify, commit, or push those files through the existing pipeline
editor or write endpoint.

#### Scenario: View a remote pipeline

- **WHEN** a user opens the pipeline view for a registered project
- **THEN** Kestrix SHALL acquire and display the pipeline at the resolved remote
  revision
- **AND** SHALL identify the requested ref and resolved commit without exposing
  a cache path

#### Scenario: Attempt to save a remote pipeline

- **WHEN** a client invokes the existing pipeline write operation for a
  registered project
- **THEN** Kestrix SHALL reject the operation as unsupported
- **AND** the UI SHALL direct the user to commit the change in the Git repository
