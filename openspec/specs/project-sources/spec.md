# project-sources Specification

## Purpose
Defines the derived set of repositories a registered project listens to, so a
push to any repository the pipeline declares can reach that project's triggers.

## Requirements

### Requirement: Sources are derived from the project and its pipeline

Every registered project SHALL have a set of sources consisting of exactly:
its primary repository (`repo_url`), marked primary, and one entry for each
`ci_repo` block in its parsed pipeline that declares a `url`. Kestrix SHALL
recompute the set whenever the project is created, updated, or its pipeline
definition is loaded from Git **at its configured ref**, replacing the previous
set in full. A load of any other revision, such as the immutable SHA of a push
to the primary repository, SHALL NOT change the set. A source
SHALL NOT be created, edited, or deleted directly through the API.

#### Scenario: Registration derives the primary source

- **WHEN** a project is registered with `repo_url` and a pipeline that declares no remote `ci_repo`
- **THEN** the project SHALL have exactly one source, marked primary, for `repo_url`

#### Scenario: Remote ci_repo blocks become sources

- **WHEN** a project's pipeline declares `ci_repo "a" { url = "https://git.example.test/team/a.git" ref = "main" }` and `ci_repo "b" { path = "services/b" }`
- **THEN** the project SHALL have a source for the primary repository and a source for `a`
- **AND** SHALL NOT have a source for `b`

#### Scenario: Refresh removes a dropped ci_repo

- **WHEN** a pipeline that declared remote `ci_repo "a"` is changed in Git to no longer declare it
- **AND** Kestrix next loads that project's pipeline definition
- **THEN** the project SHALL no longer have a source for `a`

#### Scenario: Sources are not writable

- **WHEN** a client submits `sources` in a project create or update payload
- **THEN** Kestrix SHALL ignore the submitted value
- **AND** the stored sources SHALL be the derived set

### Requirement: Repository URLs match a source independently of transport

Kestrix SHALL match a repository URL to a source by comparing a
transport-neutral key: lowercase host and repository path, ignoring the URL
scheme, an SSH user name, a `.git` suffix, and a trailing slash. A URL with
embedded credentials SHALL be rejected rather than matched.

#### Scenario: HTTPS and SSH forms match the same source

- **WHEN** a project's source is `https://git.example.test/team/a.git`
- **AND** a delivery reports the repository as `git@git.example.test:team/a.git`
- **THEN** Kestrix SHALL match the delivery to that source

#### Scenario: Different paths do not match

- **WHEN** a project's source is `https://git.example.test/team/a.git`
- **AND** a delivery reports `https://git.example.test/team/a-fork.git`
- **THEN** Kestrix SHALL NOT match the delivery to that source

#### Scenario: Embedded credentials are rejected

- **WHEN** a repository URL carrying userinfo is compared against the sources
- **THEN** Kestrix SHALL treat it as no match and SHALL NOT store or log the URL

### Requirement: One repository may be a source of several projects

Kestrix SHALL allow the same repository to be a source of more than one
project, and a lookup by repository URL SHALL return every project that
declares it. A repository SHALL be at most one source within a single project.

#### Scenario: Shared library is declared by two projects

- **WHEN** projects `checkout` and `billing` both declare `ci_repo "shared" { url = "https://git.example.test/team/shared.git" }`
- **AND** Kestrix looks up `https://git.example.test/team/shared.git`
- **THEN** the lookup SHALL return both `checkout` and `billing`

#### Scenario: Duplicate declaration within one project collapses

- **WHEN** one pipeline declares two remote `ci_repo` blocks with the same repository URL
- **THEN** the project SHALL have one source for that URL

### Requirement: Sources are exposed read-only

`GET /api/projects/{name}` SHALL include a `sources` list. Each entry SHALL
carry the `ci_repo` name (the project name for the primary), the repository
URL as declared, and whether it is primary. Entries SHALL NOT carry credential
values. `GET /api/projects` MAY omit `sources`.

#### Scenario: Project detail lists sources

- **WHEN** an authenticated `viewer` requests `GET /api/projects/checkout`
- **THEN** the response SHALL contain `sources` with the primary entry first
- **AND** no entry SHALL contain a token, password, or private key

### Requirement: Existing projects gain sources without operator action

On startup Kestrix SHALL ensure every existing project has its primary source
recorded. Secondary sources SHALL appear the first time the project's pipeline
definition is loaded after the upgrade.

#### Scenario: Upgrade backfills the primary source

- **WHEN** Kestrix starts against a database whose projects predate this change
- **THEN** every project SHALL have a primary source for its `repo_url`
- **AND** a push to that repository SHALL resolve the project as before

#### Scenario: Secondary push before first refresh

- **WHEN** a project predates this change and its pipeline has not been loaded since the upgrade
- **AND** a push arrives from a repository its pipeline declares as a remote `ci_repo`
- **AND** the delivery verifies against the global webhook secret
- **THEN** Kestrix SHALL record the delivery as `ignored` with reason `unknown project`
