# Spec: project-registration (MVP)

## Summary
Register GitLab projects with MetaCI so the webhook handler knows which pipeline to run, which GitLab project to trigger, and which secret to verify.

## Requirements

### REQ-1: Project config includes GitLab binding
Each project configuration SHALL include GitLab-specific fields.

**WHEN** a project is created via API or config file  
**THEN** it SHALL have:
- `name` (string, required) — project identifier
- `repo_path` (string) — local path to pipeline.tf directory
- `gitlab_project_id` (int, optional) — GitLab project numeric ID
- `gitlab_url` (string, optional) — GitLab instance URL (default: global config)
- `default_branch` (string, default: "main") — branch to trigger
- `webhook_secret` (string, optional) — per-project HMAC secret
- `created_at` (string) — RFC 3339 timestamp

### REQ-2: Project persistence
Project configurations SHALL survive server restarts.

**WHEN** the server starts  
**THEN** it SHALL load projects from `~/.metaci/projects.json`

**WHEN** a project is created, updated, or deleted  
**THEN** the change SHALL be written to `~/.metaci/projects.json` atomically

**WHEN** `~/.metaci/projects.json` does not exist  
**THEN** the server SHALL create it on first write

### REQ-3: Project lookup by GitLab project ID
The webhook handler SHALL find projects by GitLab project ID.

**WHEN** a webhook payload contains `project.id`  
**THEN** the server SHALL look up the project by `gitlab_project_id`

**WHEN** no project matches  
**THEN** the server SHALL return HTTP 200 with `{"status": "ignored", "reason": "unknown project"}`

### REQ-4: Auto-load from pipeline_dir
The server SHALL auto-create a project from `--dir` on startup.

**WHEN** `--dir` is specified AND `pipeline.tf` exists in that directory  
**THEN** the server SHALL create a project config pointing at that directory  
**AND** use the project name from the HCL `project` block

**WHEN** `--dir` contains multiple `.tf` files  
**THEN** only `pipeline.tf` SHALL be used for auto-load

### REQ-5: API CRUD for projects
Existing CRUD endpoints SHALL be extended with new fields.

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
