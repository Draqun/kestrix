# Tasks: Kestrix MVP — Deploy Pipeline

## MVP Tasks (deploy pipeline end-to-end)

### Task 1: File Store — persistent project config ⏱ ~1h
- [ ] Create `internal/store/filestore.go`
  - `Load()`, `Save()`, `Get()`, `Upsert()`, `Delete()`
  - Atomic write (temp file + rename)
  - Default path: `~/.metaci/projects.json`
- [ ] Write `internal/store/filestore_test.go`
  - Create, read, update, delete
  - Atomic write (concurrent safety)
  - Missing file → empty map
- [ ] Integrate into `Server`: replace `s.projects` with `FileStore`
- [ ] Verify: `make test` passes, projects persist across server restart

### Task 2: Extended ProjectConfig — GitLab binding ⏱ ~30min
- [ ] Extend `ProjectConfig` struct:
  - `GitLabProjectID int` (`json:"gitlab_project_id"`)
  - `DefaultBranch string` (`json:"default_branch"`)
  - `WebhookSecret string` (`json:"webhook_secret,omitempty"`)
- [ ] Update `handleCreateProject` to accept new fields
- [ ] Update `handleGetProject` to return new fields (mask webhook_secret)
- [ ] Update API docs (swagger annotations)
- [ ] Extend e2e test `TestProjectLifecycle` with new fields

### Task 3: GitLab client — pipeline trigger ⏱ ~1.5h
- [ ] Rewrite `internal/dispatch/client.go`:
  - `GitLabClient` struct (baseURL, token, httpClient)
  - `TriggerPipeline(projectID int, ref string, vars map[string]string) (*Pipeline, error)`
  - `GetPipelineStatus(projectID, pipelineID int) (*PipelineStatus, error)`
  - Error handling (404, 401, 500)
- [ ] Write `internal/dispatch/client_test.go`
  - Mock HTTP server for GitLab API
  - Trigger success/failure scenarios
  - Status polling
- [ ] Add `--dry-run` flag to `serve` command

### Task 4: Webhook → dispatch integration ⏱ ~1.5h
- [ ] Modify `handleWebhook` in `webhook.go`:
  - After resolve, look up project by `event.ProjectID` (from push payload)
  - If project has `GitLabProjectID`, call `TriggerPipeline()`
  - Add dispatch result to response
- [ ] Extend `PushEvent` to include `ProjectID` from GitLab payload
- [ ] Extend webhook response: `{"dispatched": bool, "pipeline_url": string, "pipeline_id": int}`
- [ ] Write e2e test: webhook → resolve → dispatch (with mock GitLab)

### Task 5: Config file — server configuration ⏱ ~1h
- [ ] Create `internal/config/config.go`:
  - Load `~/.metaci/config.yaml`
  - Env var interpolation (`${VAR}` or `${VAR:-default}`)
  - CLI flag overrides
- [ ] Config structure: `server`, `gitlab`, `webhook`, `projects`
- [ ] Update `main.go` to load config file before CLI flags
- [ ] Update `serve` command to use config values as defaults
- [ ] Write `internal/config/config_test.go`

### Task 6: Integration test — full flow ⏱ ~1h
- [ ] Write `internal/web/integration_test.go`:
  - Start server with mock GitLab API
  - Register project with gitlab_project_id
  - Send webhook push event
  - Assert: pipeline triggered, correct ref, correct variables
- [ ] Write `testutil/mock_gitlab.go` — reusable mock for tests
- [ ] Run `make quality` — all tests pass

### Task 7: Documentation + commit ⏱ ~30min
- [ ] Update README with MVP usage:
  - How to register a project
  - How to configure webhook
  - How to run locally
- [ ] Update CLAUDE.md with new architecture
- [ ] Regenerate swagger docs
- [ ] Final `make quality` gate
- [ ] Commit + push

## Follow-ups (post-MVP)

### F1: File Push Dispatch (Option B)
- Push generated `.gitlab-ci.yml` to repo via GitLab Files API
- Creates commits in user repo
- Needed for: repos without existing CI, full pipeline control

### F2: Postgres Persistence
- Replace FileStore with Postgres
- Webhook log history (queryable)
- Pipeline execution history
- Audit trail

### F3: Webhook Log History
- Store all webhook events in DB
- UI: WebhookLogs page shows real data
- Filtering by project, date, status

### F4: Multi-Environment Promotion
- `deploy_staging` → auto/manual → `deploy_production`
- Environment-specific variables
- Approval gates with notification

### F5: Per-Project Webhook Registration
- API endpoint to register webhook on GitLab
- `POST /api/projects/:name/register-webhook`
- Auto-create webhook with correct URL + secret

### F6: RBAC / Multi-User Auth
- JWT tokens for API access
- Project-level permissions (viewer, developer, admin)
- API key management

### F7: Notification System
- Slack/Teams webhook on pipeline status
- Email on failure
- Configurable per-project

### F8: Artifact Handling
- Pass artifacts between stages (build → test → deploy)
- S3/minio storage backend
- Cache for dependencies (npm, go modules)

### F9: Runner Tags and Selection
- Map `ci_repo` to GitLab runner tags
- Runner health checks
- Load balancing across runners

### F10: Automatic Webhook Registration
- On project creation, auto-register webhook on GitLab
- Verify webhook connectivity
- Handle webhook secret rotation

### F11: Terraform/OpenTofu Provider
- `metaci_project` resource
- `metaci_pipeline` data source
- `tofu plan` shows pipeline changes

### F12: Variable Secrets Integration
- JIT injection from HashiCorp Vault
- AWS Secrets Manager support
- Encrypted variable storage

### F13: CLI Enhancements
- `metaci projects list` — show registered projects
- `metaci dispatch --project X --changed Y` — manual trigger
- `metaci logs --project X` — show recent webhooks

### F14: Pipeline Execution Dashboard
- Real-time pipeline status in UI
- Job logs streaming
- Retry/abort controls

### F15: Multi-Repo Workspace Clone
- Clone all declared repos into workspace before execution
- Cross-repo script execution
- Workspace cleanup
