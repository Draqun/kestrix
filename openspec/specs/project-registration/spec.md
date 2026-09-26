# Spec: project-registration (MVP)

## Purpose
Project registration enables storing GitLab project metadata and linking webhook events to pipeline definitions.

## Summary
Register GitLab projects with Kestrix so the webhook handler knows which pipeline to run, which GitLab project to trigger, and which secret to verify.
## Requirements
### Requirement: REQ-1: Project config includes GitLab binding (implemented)
Each project configuration SHALL include its existing GitLab binding fields and
MAY include a provider-neutral `repo_url`, `repo_ref`, and
`repo_credential_ref` for a remote source. A remote project SHALL provide both
`repo_url` and a non-empty `repo_ref`; local projects SHALL continue to use
`repo_path`. Credential values SHALL NOT be accepted in project definitions.

`repo_url` SHALL be accepted only in one of three forms: an `https://` URL, an
`ssh://` URL, or an scp-like `user@host:path` address. Any other scheme
(including plaintext `http://`, `file://`, `git://`, and transport helpers such
as `ext::`) SHALL be rejected. The host SHALL be rejected when it is a literal
loopback address (`127.0.0.0/8`, `::1`, the name `localhost`), a literal
link-local address (`169.254.0.0/16`, `fe80::/10`), or the unspecified address —
including any of those addresses written in a non-canonical literal form
(decimal, hexadecimal, octal, or short-form IPv4, e.g. `2130706433` or
`0x7f000001` for `127.0.0.1`). These checks SHALL run before any network
access and SHALL apply to both project creation and project update. A rejected `repo_url` SHALL fail with a
validation error that names the rule, not the address class table.

#### Scenario: Project config includes required fields
- **WHEN** a project is created via API or config file
- **THEN** it SHALL have:
  - `name` (string, required)
  - `repo_path` (string)
  - `gitlab_project_id` (int, optional)
  - `gitlab_url` (string, optional)
  - `default_branch` (string, default: "main")
  - `webhook_secret` (string, optional)
  - `created_at` (string)

**WHEN** a project is created via API or config file  
**THEN** it SHALL have:
- `name` (string, required) — project identifier
- `repo_path` (string) — local path to pipeline.tf directory
- `gitlab_project_id` (int, optional) — GitLab project numeric ID
- `gitlab_url` (string, optional) — GitLab instance URL (default: global config)
- `default_branch` (string, default: "main") — branch to trigger
- `webhook_secret` (string, optional) — per-project HMAC secret
- `created_at` (string) — RFC 3339 timestamp

#### Scenario: Register a remote project

- **WHEN** a project is created with `name`, `repo_url`, `repo_ref`, and an
  optional `repo_credential_ref`
- **THEN** Kestrix SHALL persist the source declaration and credential reference
- **AND** it SHALL not persist any credential value

#### Scenario: Reject an incomplete remote project

- **WHEN** a project has `repo_url` without `repo_ref`
- **THEN** project validation SHALL fail before persistence
- **AND** the response SHALL explain that the remote ref is required

#### Scenario: Accepted URL forms

- **WHEN** a project is created with `repo_url` of `https://git.example.test/team/app.git`, `ssh://git@git.example.test/team/app.git`, or `git@git.example.test:team/app.git`
- **THEN** validation SHALL accept each form

#### Scenario: Rejected scheme

- **WHEN** a project is created or updated with a `repo_url` using `http://`, `file://`, `git://`, or `ext::`
- **THEN** validation SHALL fail with `400` before any clone, fetch, or DNS lookup is attempted

#### Scenario: Rejected loopback or link-local host

- **WHEN** a project is created or updated with a `repo_url` whose host is `localhost`, `127.0.0.1`, `[::1]`, `169.254.169.254`, or `[fe80::1]`
- **THEN** validation SHALL fail with `400` before any network access

#### Scenario: Rejected alternate-encoding loopback or link-local host

- **WHEN** a project is created or updated with a `repo_url` whose host is a decimal, hexadecimal, octal, or short-form literal for `127.0.0.1` or `169.254.169.254` (for example `2130706433`, `0x7f000001`, `0177.0.0.1`, or `127.1`)
- **THEN** validation SHALL fail with `400` before any network access, identically to the canonical dotted-quad form

#### Scenario: Private network host is allowed

- **WHEN** a project is created with `repo_url` `https://10.0.0.5/team/app.git`
- **THEN** validation SHALL accept it, since self-hosted Git servers commonly use RFC 1918 addresses

### Requirement: REQ-2: Project persistence (implemented)
Project configurations SHALL survive server restarts by being stored in PostgreSQL. The server SHALL not load or write project configurations through a JSON FileStore.

#### Scenario: Persistence across restarts
- **WHEN** the server starts with a working PostgreSQL database
- **THEN** it SHALL load registered projects from PostgreSQL

#### Scenario: Project mutation
- **WHEN** a project is created, updated, or deleted
- **THEN** the change SHALL be persisted in PostgreSQL
- **AND** subsequent API requests SHALL observe the change

#### Scenario: PostgreSQL is unavailable
- **WHEN** the server cannot connect to PostgreSQL
- **THEN** it SHALL fail startup
- **AND** it SHALL not create or read a JSON project store

### Requirement: REQ-3: Project lookup by GitLab project ID (implemented)
The webhook handler SHALL find projects first by matching the delivery's
repository URLs against every project's derived sources, and SHALL fall back
to the GitLab project ID when no source matches. A URL match MAY return more
than one project; an ID match returns at most one.

#### Scenario: Lookup by GitLab project ID
- **WHEN** a webhook payload contains `project.id`
- **AND** no project source matches the payload's repository URLs
- **THEN** the server SHALL look up the project by `gitlab_project_id`
- **WHEN** no project matches
- **THEN** the server SHALL return HTTP 200 with `{"status": "ignored", "reason": "unknown project"}`

#### Scenario: Lookup by source URL takes precedence
- **WHEN** a webhook payload's repository URL matches a source of project `checkout`
- **AND** the payload's `project.id` equals a different project's `gitlab_project_id`
- **THEN** the server SHALL resolve the delivery to `checkout`
- **AND** SHALL NOT also resolve the project matched by ID

**WHEN** a webhook payload contains `project.id`  
**THEN** the server SHALL look up the project by `gitlab_project_id` only after the source URL match found nothing

**WHEN** no project matches  
**THEN** the server SHALL return HTTP 200 with `{"status": "ignored", "reason": "unknown project"}`

### Requirement: REQ-4: Explicit project registration
The server SHALL NOT create or update a registered project as a side effect of startup or server construction based solely on `--dir` or `pipeline_dir`.

#### Scenario: Startup does not register a pipeline directory
- **WHEN** `--dir` is specified and `pipeline.tf` exists in that directory
- **THEN** the server SHALL NOT create or update a project row for the HCL `project` block

#### Scenario: Deleted project remains deleted
- **WHEN** a project is deleted through the project API and the server restarts with a pipeline directory configured
- **THEN** the deleted project SHALL remain absent from the project list

#### Scenario: Project is registered through the API
- **WHEN** a client creates a project through `POST /api/projects`
- **THEN** the server SHALL persist the project and expose it through the project API

### Requirement: REQ-5: API CRUD for projects (implemented)
Existing project CRUD endpoints SHALL accept and expose non-secret remote
source metadata, including `repo_url`, `repo_ref`, and `repo_credential_ref`.
Credential values SHALL be write-only through dedicated credential operations
and SHALL never appear in project or run responses.

The per-project `webhook_secret` SHALL likewise be write-only: it MAY be
supplied on create and update, SHALL be stored, and SHALL NOT appear in any
project list, detail, create, or update response. Each project response SHALL
instead carry a boolean `has_webhook_secret`. An update whose `webhook_secret`
is absent or empty SHALL keep the stored secret unchanged.

#### Scenario: API CRUD for projects
- **WHEN** `POST /api/projects` receives a payload with `name`, `repo_path`, and `gitlab_project_id`
- **THEN** the project SHALL be created with all fields persisted
- **WHEN** `GET /api/projects` returns project list
- **THEN** each project SHALL include `gitlab_project_id`, `default_branch`, and `has_webhook_secret`, and SHALL NOT include `webhook_secret`

**WHEN** `POST /api/projects` receives `{"name": "x", "repo_path": "/path", "gitlab_project_id": 12345}`  
**THEN** the project SHALL be created with all fields persisted

**WHEN** `GET /api/projects` returns project list  
**THEN** each project SHALL include `gitlab_project_id`, `default_branch`, `has_webhook_secret`

#### Scenario: Update a remote source reference

- **WHEN** a client updates `repo_ref` through project CRUD
- **THEN** the new requested ref SHALL be persisted
- **AND** future runs SHALL use the new ref without exposing credential data

#### Scenario: Webhook secret is never returned

- **WHEN** a project is created with a `webhook_secret` and then read through the list or detail endpoint
- **THEN** the response SHALL carry `has_webhook_secret: true`
- **AND** SHALL NOT carry the `webhook_secret` field or its value in any form

#### Scenario: Update without a secret keeps the stored one

- **WHEN** a client updates a project that has a webhook secret and the update payload omits `webhook_secret` or sends it empty
- **THEN** the stored secret SHALL remain unchanged
- **AND** a subsequent delivery verifying against it SHALL still be accepted

#### Scenario: Update with a new secret rotates it

- **WHEN** a client updates a project with a non-empty `webhook_secret`
- **THEN** the new value SHALL replace the stored one
- **AND** a delivery presenting the old value SHALL be rejected

### Requirement: REQ-6: Project deletion guards against an active run (implemented)

Deleting a project whose run is currently executing SHALL be refused with
`409 Conflict` naming the run(s) still active, rather than deleting the
project and letting the run's underlying process continue against rows that
no longer exist. "Currently executing" means a run with a live execution
goroutine, or a `remote`-mode run with at least one outstanding
(`pending`, `claimed` or `running`) job — a remote run has no goroutine by
design, but a runner is still executing against its rows. A run waiting for
manual approval has neither and SHALL NOT block the delete. When Kestrix
cannot determine whether a run has outstanding remote jobs, the delete SHALL
be refused rather than allowed. A project with no active run SHALL delete
exactly as before, cascading its runs and their task results.

#### Scenario: Delete refused while a run is active

- **WHEN** a client sends `DELETE /api/projects/{name}` and a run for that project is currently executing
- **THEN** the server SHALL respond `409 Conflict` naming the active run's ID
- **AND** the project, its runs, and their task results SHALL remain unchanged

#### Scenario: Delete succeeds once no run is active

- **WHEN** a client sends `DELETE /api/projects/{name}` and no run for that project is currently executing
- **THEN** the server SHALL delete the project
- **AND** its runs and their task results SHALL cascade-delete as before

#### Scenario: Cancel then delete

- **WHEN** an active run is cancelled and its process has terminated
- **AND** a client then sends `DELETE /api/projects/{name}`
- **THEN** the server SHALL delete the project, since no run is active any longer

#### Scenario: Delete refused while a remote job is outstanding

- **WHEN** a client sends `DELETE /api/projects/{name}` and a `remote`-mode run for that project has a job a runner has not yet completed
- **THEN** the server SHALL respond `409 Conflict` naming that run
- **AND** the project, its runs, and their task results SHALL remain unchanged

#### Scenario: Delete refused when outstanding jobs cannot be determined

- **WHEN** the lookup for outstanding remote jobs fails
- **THEN** the server SHALL refuse the delete rather than allowing it

## Scenarios

### Scenario: Register project with GitLab
```
GIVEN no projects exist
WHEN POST /api/projects with {"name": "my-app", "repo_path": "/src/my-app", "gitlab_project_id": 9999, "default_branch": "develop"}
THEN project is created
AND GET /api/projects returns project with gitlab_project_id=9999
AND projects.json contains the project
```

### Scenario: Server restart preserves projects
```
GIVEN project "my-app" exists in projects.json
WHEN server restarts
THEN GET /api/projects returns project "my-app"
AND project config matches pre-restart values
```

### Scenario: Webhook from unknown project
```
GIVEN no project with gitlab_project_id=7777
WHEN webhook arrives with project.id=7777
THEN response is {"status": "ignored", "reason": "unknown project"}
AND no pipeline is triggered
```
