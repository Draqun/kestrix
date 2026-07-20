# Proposal: MetaCI MVP — Deploy Pipeline

## Why

MetaCI PoC has all the building blocks (parser, DAG, generator, webhook, UI) but they are disconnected. A webhook arrives, a result is computed — and nothing happens. The MVP connects these blocks into a single flow: **push → webhook → parse → resolve → dispatch → deploy**.

The goal is to reach a state where a user can:
1. Define a pipeline in HCL
2. Point a GitLab repo at MetaCI
3. Push code and see a real deployment happen end-to-end

## What Changes

### Webhook → Dispatch Pipeline (the critical path)
The webhook handler currently returns JSON and stops. In MVP it continues:
1. Parse `pipeline.tf` from the project's configured repo
2. Resolve triggers against changed files
3. Generate `gitlab-ci.yml` from resolved tasks
4. Push the generated YAML to the repo (branch or direct) OR trigger via GitLab Pipeline API
5. GitLab Runner picks up the pipeline and executes jobs
6. Return execution status in webhook response

### GitLab Project Registration
MetaCI must know each project's GitLab project ID, URL, and webhook secret. The in-memory `ProjectConfig` expands with:
- `gitlab_project_id` (int) — for API calls
- `gitlab_url` (string) — instance URL
- `webhook_secret` (string) — per-project HMAC key
- `default_branch` (string) — usually "main"

### Minimal State Persistence
Project configs survive server restart. Minimum: JSON file on disk (`~/.metaci/projects.json`). No Postgres yet.

## Capabilities

### New Capabilities
- **dispatch-pipeline**: Complete webhook → parse → resolve → generate → execute flow
- **project-registration**: GitLab project binding (ID, URL, secrets, branch)

### Modified Capabilities
- **webhook-receiver**: After resolving triggers, calls dispatch instead of returning
- **ci-generator**: Outputs to file or triggers GitLab API directly

## Non-Goals (MVP)
- Postgres / database
- RBAC / multi-user auth
- Multi-environment promotion (staging → production)
- Artifact handling between jobs
- Slack/Teams notifications
- Automatic webhook registration
- Terraform/OpenTofu provider
- Mobile SDK / data pipeline patterns

These are follow-ups.

## Impact
- **Code**: New `internal/dispatch/` (rewrite), new `internal/store/` (file-based persistence), modified `internal/web/webhook.go` + `server.go`
- **External**: Requires GitLab API token with `api` scope, GitLab Runner registered and available
- **Security**: Webhook secrets per-project, API tokens stored in env vars or config file
