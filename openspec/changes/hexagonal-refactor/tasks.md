## Phase 1 — Domain Types (non-behavioral)

### Red — write tests first
- [x] 1.1 Write `domain_test.go`: JSON round-trip test for `domain.TaskResult` ( marshal → unmarshal → assert fields)
- [x] 1.2 Write `domain_test.go`: JSON round-trip test for `model.Project` with new JSON tags (assert field names match current `PipelineSnapshot` output)

### Green — implement
- [x] 1.3 Create `internal/domain` package with `TaskResult` struct (merge `runner.TaskResult` + `store.TaskResult` fields, add JSON tags)
- [x] 1.4 Add JSON tags to `model.Task`, `model.Repo`, `model.ExecuteSpec`, `model.Variable`, `model.Project` to match current `PipelineSnapshot` field names
- [x] 1.5 Replace `runner.TaskResult` with `domain.TaskResult` across `runner.go`, `server.go`, tests
- [x] 1.6 Replace `store.TaskResult` with `domain.TaskResult` across `pgstore.go`, `filestore.go`
- [x] 1.7 Remove `store.PipelineSnapshot`, `store.SnapshotRepo`, `store.SnapshotTask`, `store.SnapshotExec`, `store.SnapshotVar`, `store.SnapshotFromProject`

### Refactor — verify
- [x] 1.8 `go build && go test ./... -count=1 -race` passes

## Phase 2 — Ports + Store Split

### Red — write tests first
- [x] 2.1 Write `domain/portscheck`: compile-time assertions `var _ domain.ProjectStore = (*store.FileStore)(nil)` and `var _ domain.ProjectStore = (*store.PGStore)(nil)`
- [x] 2.2 Write `domain/portscheck`: compile-time assertion `var _ domain.RunStore = (*store.PGStore)(nil)`
- [x] 2.3 Write `domain/mocks.go`: `MockProjectStore`, `MockRunStore`, `MockPipelineRunner`, `MockWebhookLogSink` with configurable returns + call recording
- [x] 2.4 Write `domain/mocks_test.go`: test each mock records calls and returns configured values correctly

### Green — implement
- [x] 2.5 Define `domain.ProjectStore` interface (Load, Save, Get, FindByGitLabID, Upsert, Delete, List) in `internal/domain`
- [x] 2.6 Define `domain.RunStore` interface (CreateRun, FinishRun, AppendTaskResult, FlushRunOutput, CancelRun, ApplyTaskResult, GetRun, CountRuns, ListRuns, ListAllRuns, CountRunsByStatus) in `internal/domain`
- [x] 2.7 Define `domain.PipelineRunner` interface (Run, with `RunnerOptions` in domain) in `internal/domain`
- [x] 2.8 Define `domain.WebhookLogSink` interface (Append, List) in `internal/domain`
- [ ] 2.9 Split `pgstore.go` into `pgstore_project.go` (ProjectStore impl) + `pgstore_run.go` (RunStore impl)
- [ ] 2.10 Make `FileStore` implement `domain.ProjectStore` only (remove `ListAllRuns` stub)
- [ ] 2.11 Update `Server` to hold `domain.ProjectStore` + optional `domain.RunStore` (nil for FileStore) + `domain.PipelineRunner`
- [ ] 2.12 Remove all `s.store.(*store.PGStore)` type assertions — use `s.runStore` (nil check) instead

### Refactor — verify
- [x] 2.13 `go build && go test ./... -count=1 -race` passes

## Phase 3 — Service Layer

### Red — write tests first
- [ ] 3.1 Write `service/project_service_test.go`: `TestProjectService_Create_MissingName` — mock `ProjectStore`, assert error before Upsert called
- [ ] 3.2 Write `service/project_service_test.go`: `TestProjectService_Create_MissingRepo` — mock, assert error before Upsert called
- [ ] 3.3 Write `service/project_service_test.go`: `TestProjectService_Create_Happy` — mock, assert Upsert called with correct config
- [ ] 3.4 Write `service/run_service_test.go`: `TestRunService_Start_Happy` — mock `RunStore` + `PipelineRunner`, assert CreateRun called, runner executed, FinishRun called with "passed"
- [ ] 3.5 Write `service/run_service_test.go`: `TestRunService_Start_PanicRecovered` — mock `PipelineRunner` panics, assert run marked "failed"
- [ ] 3.6 Write `service/run_service_test.go`: `TestRunService_Start_TaskFailed` — mock runner returns failed task result, assert FinishRun called with "failed"
- [ ] 3.7 Write `service/webhook_service_test.go`: `TestWebhookService_Process_Happy` — mock log sink, assert event parsed and logged
- [ ] 3.8 Write `service/var_store_test.go`: `TestBuildVarStore_Happy` — assert var store built with project variables, env, config vars
- [ ] 3.9 Write `service/coverage_test.go`: `TestServiceCoverageAbove90` — fail if package coverage < 90%

### Green — implement
- [ ] 3.10 Create `internal/service` package
- [ ] 3.11 Implement `ProjectService` — CRUD validation (name required, repo required), delegates to `domain.ProjectStore`
- [ ] 3.12 Implement `RunService` — owns `CreateRun` + background goroutine + `FinishRun`; accepts `domain.RunStore` + `domain.PipelineRunner`; goroutine has `defer recover` → mark run failed
- [ ] 3.13 Implement `buildVarStore(proj, cfg) *variables.Store` helper in `service` — eliminates 3× duplication
- [ ] 3.14 Implement `WebhookService` — event parsing, classifier, dispatch logic; delegates to `domain.WebhookLogSink`
- [ ] 3.15 Refactor all handlers in `server.go` to call services instead of doing business logic inline
- [ ] 3.16 Verify all service tests pass with mocks (no PG, no filesystem)

### Refactor — verify
- [ ] 3.17 `internal/service` does not import `net/http` — verify with `go list -deps ./internal/service | grep net/http` (should be empty)
- [ ] 3.18 `go build && go test ./... -count=1 -race` passes
- [ ] 3.19 `go test ./internal/service/ -coverprofile=coverage.out && go tool cover -func=coverage.out` reports ≥90%

## Phase 4 — Runner Interface + HTTP Adapter Split

### Red — write tests first
- [ ] 4.1 Write `runner/executor_test.go`: test `taskLoop` with a mock `Executor` — asserts manual-skip and dependency-skip logic works correctly
- [ ] 4.2 Write `runner/executor_test.go`: test each executor satisfies `domain.PipelineRunner` via compile-time assertion
- [ ] 4.3 Write `web/pagination_test.go`: test `Parse` with valid/invalid params, test `Links` builds correct next/prev URLs
- [ ] 4.4 Write `web/errwrite_test.go`: test `writeError` produces correct status code + JSON body
- [ ] 4.5 Write `web/handlers/system_test.go`: `TestHandleGetLogs_Concurrent` — two goroutines appending to webhookLog, verify no race with `-race`
- [ ] 4.6 Write `web/handlers/coverage_test.go`: `TestHandlerCoverageAbove85` — fail if handlers package coverage < 85%

### Green — implement
- [ ] 4.7 Extract `internal/web/pagination` package (Parse, Links, Meta)
- [ ] 4.8 Add `writeError(w, code, msg)` helper in `internal/web`
- [ ] 4.9 Replace all inline `http.Error(w, fmt.Sprintf(...))` with `writeError`
- [ ] 4.10 Protect `webhookLog` with `sync.RWMutex` — append uses Lock, reads use RLock
- [ ] 4.11 Split `server.go` into `handlers/project.go`, `handlers/run.go`, `handlers/pipeline.go`, `handlers/webhook.go`, `handlers/system.go`
- [ ] 4.12 Shrink `server.go` to wiring only (NewServer, buildRouter, ListenAndServe) — target under 300 lines
- [ ] 4.13 Extract `runner.Executor` interface with `LocalExecutor`, `DockerExecutor`, `GitLabRunnerExecutor` implementations
- [ ] 4.14 Extract shared `taskLoop(ordered, executor, opts)` to eliminate ~150 lines duplicated task-loop body

### Refactor — verify
- [ ] 4.15 `go build && go test ./... -count=1 -race` passes

## Phase 5 — CI Pipeline

### Red — write tests first
- [ ] 5.1 Write `scripts/test_coverage.sh`: parse `go tool cover -func` output, exit 1 if total < 80%, exit 1 if any package below its target (domain ≥90%, service ≥90%, web/handlers ≥85%, store ≥80%, runner ≥80%)
- [ ] 5.2 Write `scripts/test_coverage.sh`: test script with a mock coverage.out that is above threshold (should pass) and below threshold (should fail)

### Green — implement
- [ ] 5.3 Update `scripts/test.sh` to run `go test ./... -coverprofile=coverage.out -covermode=atomic -race -count=1`
- [ ] 5.4 Add coverage threshold check call to `scripts/test.sh` (invoke `scripts/test_coverage.sh`)
- [ ] 5.5 Update `.gitlab-ci.yml` `test` stage to upload coverage.out as cobertura report artifact
- [ ] 5.6 Verify `scripts/test.sh` passes locally with current codebase

### Refactor — verify
- [ ] 5.7 `go build && go test ./... -count=1 -race` passes
- [ ] 5.8 `scripts/test.sh` exits 0 (coverage above threshold)

## Verify

- [ ] 6.1 `server.go` under 300 lines
- [ ] 6.2 `internal/service` does not import `net/http`
- [ ] 6.3 `internal/domain` has zero `internal/*` import dependencies
- [ ] 6.4 No `*PGStore` type assertions remain in `web`
- [ ] 6.5 No `store.TaskResult` or `runner.TaskResult` — only `domain.TaskResult`
- [ ] 6.6 No `PipelineSnapshot` type — `model.Project` used directly
- [ ] 6.7 All tests pass with `-race`
- [ ] 6.8 `go test ./internal/domain/ -coverprofile` reports ≥90%
- [ ] 6.9 `go test ./internal/service/ -coverprofile` reports ≥90%
- [ ] 6.10 `go test ./internal/web/handlers/ -coverprofile` reports ≥85%
- [ ] 6.11 Total coverage ≥80%
- [ ] 6.12 `scripts/test.sh` enforces all coverage thresholds in CI