## MODIFIED Requirements

### Requirement: Push Event Parsing (implemented)

The server SHALL parse GitLab push event payloads, extracting:
- Repository path_with_namespace
- Repository URLs (HTTP and SSH clone URLs, and the web URL when present)
- GitLab project ID
- Branch name (normalized from refs/heads/* or refs/tags/*)
- Changed files (deduplicated across all commits)
- Commit count
- The immutable checkout SHA

#### Scenario: Parse a push event

```
WHEN the server receives a valid GitLab push webhook payload
THEN it SHALL extract the repo, branch, and changed files
AND the branch SHALL be normalized (refs/heads/main → main)
```

#### Scenario: Repository URLs are extracted

- **WHEN** the payload's `project` object carries `git_http_url`, `git_ssh_url`, and `web_url`
- **THEN** the parsed event SHALL carry all three
- **AND** a missing URL field SHALL be omitted rather than fail parsing

#### Scenario: Reject non-push events

```
WHEN the server receives an event with object_kind != "push"
THEN it SHALL respond with 400 Bad Request
```

### Requirement: Pipeline Loading (implemented)

For a registered project the server SHALL load the pipeline definition from
the project's primary source on each webhook request. When the delivery came
from the primary source, the definition SHALL be loaded at the event's
immutable checkout SHA. When the delivery came from a secondary source, the
definition SHALL be loaded from the primary source at the project's configured
`repo_ref`, and the event SHA SHALL be recorded against the secondary source
rather than checked out as the pipeline. Multiple .tf files in the pipeline
directory SHALL be merged into a single project. When the server runs against
a single pipeline directory with no registered project, it SHALL load that
directory as before.

#### Scenario: Load pipeline from directory

```
WHEN the server receives a webhook
AND the pipeline directory contains repos.tf and tasks.tf
THEN both files SHALL be parsed and merged into one project
```

#### Scenario: Primary push loads the event revision

- **WHEN** a push to a project's primary repository arrives with checkout SHA `abc123`
- **THEN** the pipeline definition SHALL be loaded from the primary source at `abc123`

#### Scenario: Secondary push loads the configured primary ref

- **WHEN** a push to a repository that is a secondary source of project `checkout` arrives with checkout SHA `def456`
- **THEN** the pipeline definition SHALL be loaded from `checkout`'s primary source at its configured `repo_ref`
- **AND** the recorded event revision SHALL be `def456`
- **AND** the primary source SHALL NOT be resolved to `def456`

## ADDED Requirements

### Requirement: Project resolution by source

The server SHALL resolve the projects a delivery belongs to by matching the
event's repository URLs against every project's sources. When no source
matches, it SHALL fall back to the GitLab project ID lookup. When neither
matches, it SHALL record the delivery as `ignored` and respond
`{"status": "ignored", "reason": "unknown project"}` with HTTP 200.

#### Scenario: Secondary source resolves the project

- **WHEN** a push arrives from a repository that no project has as `repo_url` but that project `checkout` declares as a remote `ci_repo`
- **THEN** the server SHALL resolve the delivery to `checkout`

#### Scenario: GitLab project ID still resolves

- **WHEN** a push arrives whose repository URLs match no source
- **AND** the payload's `project.id` equals a project's `gitlab_project_id`
- **THEN** the server SHALL resolve the delivery to that project

#### Scenario: Nothing matches

- **WHEN** a push arrives whose URLs match no source and whose `project.id` matches no project
- **THEN** the server SHALL respond HTTP 200 with `{"status": "ignored", "reason": "unknown project"}`
- **AND** SHALL record one history event with status `ignored`

### Requirement: Delivery fans out to every matching project

When a delivery matches more than one project, the server SHALL resolve,
record, and (when configured) dispatch each project independently. A failure
in one project SHALL NOT prevent the others from being processed. The
response SHALL list each project's outcome.

#### Scenario: Shared repository triggers two projects

- **WHEN** a push arrives from a repository that is a source of both `checkout` and `billing`
- **THEN** the server SHALL evaluate each project's DAG against its own changed set
- **AND** SHALL record one history event per project
- **AND** the response SHALL contain one result per project

#### Scenario: One project fails to load

- **WHEN** a delivery matches `checkout` and `billing` and `billing`'s pipeline fails to parse
- **THEN** `checkout` SHALL still be resolved and recorded
- **AND** `billing` SHALL be recorded with status `error`
- **AND** the response SHALL report both outcomes

### Requirement: Changed set for a secondary push

When the delivery came from a secondary source, the server SHALL mark the
`ci_repo` matching that source as changed when the push contains at least one
code-classified file, and SHALL NOT apply path-prefix matching of the push's
files against the primary repository's subpath `ci_repo` blocks.

#### Scenario: Secondary push marks its ci_repo changed

- **WHEN** a push to secondary source `shared` touches `lib/util.go`
- **THEN** the changed set SHALL be exactly `["shared"]`
- **AND** a subpath `ci_repo "lib" { path = "lib" }` of the primary repository SHALL NOT be marked changed

#### Scenario: Secondary push with only excluded files

- **WHEN** a push to a secondary source touches only `README.md`
- **THEN** the changed set SHALL be empty
- **AND** the delivery SHALL be recorded with status `skipped`

### Requirement: History records the matched source

Each recorded webhook event SHALL carry the name of the source it matched
(the project name for the primary source, the `ci_repo` name otherwise), and
`GET /api/logs` SHALL return it as `source_name`.

#### Scenario: Secondary delivery is attributed

- **WHEN** a push from secondary source `shared` is recorded for project `checkout`
- **THEN** the history item SHALL have `project_name` `checkout` and `source_name` `shared`
