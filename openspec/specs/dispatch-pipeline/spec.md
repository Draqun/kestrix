# Spec: dispatch-pipeline (MVP)

## Purpose
Dispatch pipeline triggers a GitLab pipeline after the DAG engine resolves tasks from a webhook event.

## Summary
Complete the webhook → parse → resolve → generate → dispatch flow so that a Git push actually triggers a pipeline execution on a GitLab Runner.

## Requirements

### REQ-1: Webhook triggers dispatch (implemented)
The webhook handler SHALL, after resolving triggered tasks, call the dispatch client to trigger a pipeline on GitLab.

#### Scenario: Webhook triggers dispatch
- **WHEN** a valid push webhook arrives AND the project has `gitlab_project_id` configured
- **THEN** the server SHALL generate `gitlab-ci.yml` from triggered tasks AND call `GitLabClient.TriggerPipeline()`

**WHEN** a valid push webhook arrives AND the project has `gitlab_project_id` configured  
**THEN** the server SHALL generate `gitlab-ci.yml` from triggered tasks AND call `GitLabClient.TriggerPipeline()`

**WHEN** the project does NOT have `gitlab_project_id` configured  
**THEN** the server SHALL return the result without dispatching (backward-compatible PoC mode)

### REQ-2: GitLab pipeline trigger (implemented)
The dispatch client SHALL trigger pipelines via the GitLab Pipeline API.

#### Scenario: GitLab pipeline trigger
- **WHEN** `TriggerPipeline(projectID, ref, variables)` is called
- **THEN** the client SHALL POST to `/api/v4/projects/:id/pipeline` with `ref` and optional `variables`
- **AND** return the pipeline ID and URL

**WHEN** `TriggerPipeline(projectID, ref, variables)` is called  
**THEN** the client SHALL POST to `/api/v4/projects/:id/pipeline` with `ref` and optional `variables`  
**AND** return the pipeline ID and URL

**WHEN** the GitLab API returns an error  
**THEN** the client SHALL return a descriptive error (HTTP status, message)

### REQ-3: Pipeline variables injection (implemented)
Triggered task names and changed repo list SHALL be passed as pipeline variables.

#### Scenario: Pipeline variables injection
- **WHEN** a pipeline is triggered
- **THEN** the following variables SHALL be set:
  - `METACI_TRIGGERED_TASKS` — comma-separated task names
  - `METACI_CHANGED_REPOS` — comma-separated repo names
  - `METACI_DISPATCH_TIME` — ISO 8601 timestamp

**WHEN** a pipeline is triggered  
**THEN** the following variables SHALL be set:
- `METACI_TRIGGERED_TASKS` — comma-separated task names
- `METACI_CHANGED_REPOS` — comma-separated repo names
- `METACI_DISPATCH_TIME` — ISO 8601 timestamp

### REQ-4: Dispatch response (implemented)
The webhook response SHALL include dispatch status.

#### Scenario: Dispatch response handling
- **WHEN** dispatch succeeds
- **THEN** the response SHALL include `{"pipeline_url": "...", "pipeline_id": N, "dispatched": true}`
- **WHEN** dispatch fails
- **THEN** the response SHALL include `{"dispatched": false, "error": "..."}` and HTTP status 200

**WHEN** dispatch succeeds  
**THEN** the response SHALL include `{"pipeline_url": "...", "pipeline_id": N, "dispatched": true}`

**WHEN** dispatch fails  
**THEN** the response SHALL include `{"dispatched": false, "error": "..."}` AND the HTTP status SHALL still be 200 (webhook acknowledged, dispatch retriable)

### REQ-5: Dry-run mode (implemented)
Dispatch SHALL support dry-run mode for testing.

#### Scenario: Dry-run mode
- **WHEN** `METACI_DRY_RUN=true` OR `--dry-run` flag is set
- **THEN** the dispatch client SHALL log the request WITHOUT calling GitLab API and return a mock response

**WHEN** `METACI_DRY_RUN=true` OR `--dry-run` flag  
**THEN** the dispatch client SHALL log the request WITHOUT calling GitLab API  
**AND** return a mock response

## Scenarios

### Scenario: Push triggers pipeline
```
GIVEN project "demo-mono" with gitlab_project_id=12345
AND default_branch="main"
WHEN GitLab sends push webhook with changed file "src/auth/handler.go"
AND webhook handler resolves triggered tasks ["build_auth", "test_integration"]
THEN dispatch client POSTs to /api/v4/projects/12345/pipeline
AND sets ref="main"
AND sets variable METACI_TRIGGERED_TASKS="build_auth,test_integration"
AND response includes pipeline_url and dispatched=true
```

### Scenario: Push to project without GitLab config
```
GIVEN project "local-test" WITHOUT gitlab_project_id
WHEN GitLab sends push webhook
AND webhook handler resolves triggered tasks ["build"]
THEN dispatch is NOT called
AND response includes dispatched=false
AND response still includes triggered task list
```

### Scenario: GitLab API error
```
GIVEN project with invalid gitlab_project_id
WHEN dispatch client calls GitLab API
AND GitLab returns 404
THEN dispatch client returns error "project not found"
AND webhook response includes dispatched=false and error message
```
