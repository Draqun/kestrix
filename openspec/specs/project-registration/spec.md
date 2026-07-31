# Spec: project-registration (MVP)

## Purpose
Project registration enables storing GitLab project metadata and linking webhook events to pipeline definitions.

## Summary
Register GitLab projects with Kestrix so the webhook handler knows which pipeline to run, which GitLab project to trigger, and which secret to verify.

## Requirements

### REQ-1: Project config includes GitLab binding (implemented)
Each project configuration SHALL include GitLab-specific fields.

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

### REQ-2: Project persistence (implemented)
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

### REQ-3: Project lookup by GitLab project ID (implemented)
The webhook handler SHALL find projects by GitLab project ID.

#### Scenario: Lookup by GitLab project ID
- **WHEN** a webhook payload contains `project.id`
- **THEN** the server SHALL look up the project by `gitlab_project_id`
- **WHEN** no project matches
- **THEN** the server SHALL return HTTP 200 with `{"status": "ignored", "reason": "unknown project"}`

**WHEN** a webhook payload contains `project.id`  
**THEN** the server SHALL look up the project by `gitlab_project_id`

**WHEN** no project matches  
**THEN** the server SHALL return HTTP 200 with `{"status": "ignored", "reason": "unknown project"}`

### REQ-4: Explicit project registration
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

### REQ-5: API CRUD for projects (implemented)
Existing CRUD endpoints SHALL be extended with new fields.

#### Scenario: API CRUD for projects
- **WHEN** `POST /api/projects` receives a payload with `name`, `repo_path`, and `gitlab_project_id`
- **THEN** the project SHALL be created with all fields persisted
- **WHEN** `GET /api/projects` returns project list
- **THEN** each project SHALL include `gitlab_project_id`, `default_branch`, and masked `webhook_secret`

**WHEN** `POST /api/projects` receives `{"name": "x", "repo_path": "/path", "gitlab_project_id": 12345}`  
**THEN** the project SHALL be created with all fields persisted

**WHEN** `GET /api/projects` returns project list  
**THEN** each project SHALL include `gitlab_project_id`, `default_branch`, `webhook_secret` (masked)

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
