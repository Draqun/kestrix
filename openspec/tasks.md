# Tasks: Kestrix PoC Implementation

## Completed

### Task 1: Project Bootstrap ✅
- [x] Initialize Go module (`gitlab.com/lunaris-bot/metaci`)
- [x] Create project structure (`cmd/`, `internal/`)


- [x] Add LICENSE (all rights reserved)
- [x] Initialize OpenSpec
- [x] Create Makefile with help, init, build targets

### Task 2: Domain Model ✅
- [x] Define `Project`, `Repo`, `Task`, `ExecuteSpec` types
- [x] Add HCL struct tags for parser integration
- [x] Create `ProjectFile` wrapper for root-level HCL decoding
- [x] Add `Variable` and `VariableDef` structs

### Task 3: HCL Parser ✅
- [x] Implement manual body decoding via `hclsyntax.Body`
- [x] Support `project`, `ci_repo`, `ci_task`, `execute`, `variable` blocks
- [x] Handle `cty.Value` extraction for string/bool literals
- [x] Implement `ParseFile` and `ParseDir` functions
- [x] Add error formatting with line/column info

### Task 4: DAG Engine ✅
- [x] Implement boolean expression tokenizer
- [x] Implement recursive descent parser for `&&`, `||`, `!`, parentheses
- [x] Support `repo.<name>.changed` predicates
- [x] Implement `Resolve` (evaluate triggers + return triggered nodes)
- [x] Implement `Validate` (syntax + cycle detection + unknown refs)
- [x] Write 10 unit tests covering all trigger patterns

### Task 5: CLI ✅
- [x] `metaci validate <file.tf>` — check syntax, cycles, refs
- [x] `metaci resolve --changed X,Y <file.tf>` — evaluate triggers
- [x] `metaci serve --addr :8080 --dir ./examples` — start webhook server
- [x] `metaci variables <file.tf> --repo X --job Y` — show resolved variables

### Task 6: Webhook Receiver ✅
- [x] GitLab push event parser
- [x] File classifier (code vs docs exclusion)
- [x] Chi HTTP server with `/webhook`, `/health`, `/`, `/docs/`
- [x] End-to-end webhook processing
- [x] Write 10+ unit tests

### Task 7: CI Generator ✅
- [x] Generate gitlab-ci.yml from resolved DAG nodes
- [x] Stage derivation from dependency graph
- [x] `needs` mapping for task dependencies
- [x] `when: manual` for manual tasks
- [x] Per-job container images

### Task 8: Quality Pipeline ✅
- [x] Makefile with `quality`, `test-coverage`, `check` targets
- [x] `.golangci.yml` configuration
- [x] Test coverage reporting
- [x] Format checking (`gofmt`)

### Task 9: Parser Tests ✅
- [x] Unit tests for `ParseFile` and `ParseDir`
- [x] Integration test with complex HCL
- [x] Error case testing

### Task 10: Generator Tests ✅
- [x] Unit tests for `GenerateGitLabCI`
- [x] Stage derivation tests
- [x] Manual task handling tests

### Task 11: Runner Dispatch ✅
- [x] GitLab API integration (`POST /projects/:id/pipeline`)
- [x] Dynamic YAML injection
- [x] Pipeline status polling

### Task 12: Webhook Signature Verification ✅
- [x] GitLab X-Gitlab-Token verification middleware
- [x] HMAC-SHA256 verification for generic webhooks
- [x] Secret management via env vars

### Task 13: Variables and Files Support ✅
- [x] Per-project variables (HCL `variable` blocks)
- [x] Per-repo variable overrides (scope = "repo")
- [x] Per-job variable injection (scope = "job")
- [x] Secret variables (masked in logs)
- [x] Variable store with hierarchical resolution

### Task 14: Multi-Repo Workspace ✅
- [x] Clone multiple repos into workspace
- [x] Cross-repo script execution context
- [x] Working directory management
- [x] Integration with webhook handler

## Infrastructure

### Docker Stack ✅
- [x] Production Dockerfile (multi-stage, distroless)
- [x] Dev Dockerfile (air hot reload)
- [x] docker-compose.yml (metaci + postgres + redis)
- [x] docker/Makefile (up, down, logs, rebuild, shell, test-webhook, health)

### Conventions ✅
- [x] CLAUDE.md (AI dev guide)
- [x] .better-commits.json (conventional commits)
- [x] .env.template (environment variables)
- [x] .gitlab-ci.yml (Kestrix's own CI pipeline)

### Documentation ✅
- [x] OpenSpec proposal + 4 specs + design + tasks
- [x] Swagger/OpenAPI docs at `/docs/`
- [x] README with architecture and usage

---

## MVP: Deploy Pipeline

**Full plan:** `openspec/tasks-mvp.md`  
**Design:** `openspec/design-mvp.md`  
**Specs:** `openspec/specs/dispatch-pipeline/spec.md`, `openspec/specs/project-registration/spec.md`

### Task 15: File Store — persistent project config
- [ ] Create `internal/store/filestore.go` (Load/Save/Get/Upsert/Delete)
- [ ] Atomic write (temp file + rename)
- [ ] Write `filestore_test.go`
- [ ] Integrate into Server, replace in-memory map

### Task 16: Extended ProjectConfig — GitLab binding
- [ ] Add GitLabProjectID, DefaultBranch, WebhookSecret to ProjectConfig
- [ ] Update API endpoints + swagger annotations
- [ ] Extend e2e test

### Task 17: GitLab client — pipeline trigger
- [ ] Rewrite `internal/dispatch/client.go` (GitLabClient)
- [ ] TriggerPipeline + GetPipelineStatus
- [ ] Mock HTTP test server
- [ ] Add `--dry-run` flag

### Task 18: Webhook → dispatch integration
- [ ] Modify handleWebhook: after resolve → dispatch
- [ ] Extend PushEvent with ProjectID
- [ ] Extend webhook response with dispatch result
- [ ] E2e test with mock GitLab

### Task 19: Config file
- [ ] Create `internal/config/config.go` (~/.metaci/config.yaml)
- [ ] Env var interpolation
- [ ] CLI flag overrides
- [ ] Config test

### Task 20: Integration test — full flow
- [ ] Write `integration_test.go` (webhook → dispatch)
- [ ] Create `testutil/mock_gitlab.go`
- [ ] Final quality gate

### Task 21: Documentation + commit
- [ ] Update README with MVP usage
- [ ] Update CLAUDE.md
- [ ] Regenerate swagger
- [ ] Final commit + push
