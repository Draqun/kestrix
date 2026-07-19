# Tasks: MetaCI PoC Implementation

## Completed

### Task 1: Project Bootstrap
- [x] Initialize Go module (`gitlab.com/lunaris-bot/metaci`)
- [x] Create project structure (`cmd/`, `internal/`)
- [x] Add LICENSE (all rights reserved)
- [x] Initialize OpenSpec
- [x] Create Makefile with help, init, build targets

### Task 2: Domain Model
- [x] Define `Project`, `Repo`, `Task`, `ExecuteSpec` types
- [x] Add HCL struct tags for parser integration
- [x] Create `ProjectFile` wrapper for root-level HCL decoding

### Task 3: HCL Parser
- [x] Implement manual body decoding via `hclsyntax.Body`
- [x] Support `project`, `ci_repo`, `ci_task`, `execute` blocks
- [x] Handle `cty.Value` extraction for string/bool literals
- [x] Implement `ParseFile` and `ParseDir` functions
- [x] Add error formatting with line/column info

### Task 4: DAG Engine
- [x] Implement boolean expression tokenizer
- [x] Implement recursive descent parser for `&&`, `||`, `!`, parentheses
- [x] Support `repo.<name>.changed` predicates
- [x] Implement `Resolve` (evaluate triggers + return triggered nodes)
- [x] Implement `Validate` (syntax + cycle detection + unknown refs)
- [x] Write 10 unit tests covering all trigger patterns

### Task 5: CLI
- [x] `metaci validate <file.tf>` — check syntax, cycles, refs
- [x] `metaci resolve --changed X,Y <file.tf>` — evaluate triggers
- [x] `metaci serve --addr :8080 --dir ./examples` — start webhook server

### Task 6: Webhook Receiver
- [x] GitLab push event parser
- [x] File classifier (code vs docs exclusion)
- [x] Chi HTTP server with `/webhook`, `/health`, `/ping`
- [x] End-to-end webhook processing
- [x] Write 10 unit tests

### Task 7: CI Generator
- [x] Generate gitlab-ci.yml from resolved DAG nodes
- [x] Stage derivation from dependency graph
- [x] `needs` mapping for task dependencies
- [x] `when: manual` for manual tasks
- [x] Per-job container images

### Task 8: Quality Pipeline
- [x] Makefile with `quality`, `test-coverage`, `check` targets
- [x] `.golangci.yml` configuration
- [x] Test coverage reporting
- [x] Format checking (`gofmt`)

## In Progress

### Task 9: Parser Tests
- [ ] Unit tests for `ParseFile` and `ParseDir`
- [ ] Integration test with complex HCL
- [ ] Error case testing

### Task 10: Generator Tests
- [ ] Unit tests for `GenerateGitLabCI`
- [ ] Stage derivation tests
- [ ] Manual task handling tests

## Pending

### Task 11: Runner Dispatch
- [ ] GitLab API integration (`POST /projects/:id/pipeline`)
- [ ] Dynamic YAML injection
- [ ] Pipeline status polling

### Task 12: Webhook Signature Verification
- [ ] HMAC-SHA256 verification for GitLab webhooks
- [ ] Secret management (env vars or Vault)

### Task 13: Variables and Files Support
- [ ] Per-project variables (HCL `variables` block)
- [ ] Per-repo variable overrides
- [ ] Per-job variable injection
- [ ] Secret variables (masked in logs)

### Task 14: Multi-Repo Workspace
- [ ] Clone multiple repos into workspace
- [ ] Cross-repo script execution
- [ ] Working directory management
