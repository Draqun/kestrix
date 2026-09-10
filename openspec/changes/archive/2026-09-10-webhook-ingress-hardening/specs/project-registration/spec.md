## MODIFIED Requirements

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
link-local address (`169.254.0.0/16`, `fe80::/10`), or the unspecified address.
These checks SHALL run before any network access and SHALL apply to both
project creation and project update. A rejected `repo_url` SHALL fail with a
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

#### Scenario: Private network host is allowed

- **WHEN** a project is created with `repo_url` `https://10.0.0.5/team/app.git`
- **THEN** validation SHALL accept it, since self-hosted Git servers commonly use RFC 1918 addresses

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
