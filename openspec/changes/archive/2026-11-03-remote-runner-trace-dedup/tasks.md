# Tasks: remote-runner-trace-dedup

## 1. Store Interface and Persistence Updates

- [x] 1.1 Update `domain.RemoteJobStore.AppendJobTrace` signature to return the count of newly appended bytes; update `PGStore.AppendJobTrace` and SQL queries to report appended byte counts.
- [x] 1.2 Update `MockRemoteJobStore` in `internal/mocks` to implement the new signature.

## 2. Service Layer Integration

- [x] 2.1 Add `RunService.AppendRemoteJobTrace` to coordinate trace appending and broadcast only non-zero appended byte slices to `RunEventHub`.
- [x] 2.2 In `RunService.ClaimNextJob`, broadcast a task `running` event when a pending job is assigned to a runner.
- [x] 2.3 Refactor `handleRunnerJobTrace` and `handleRunnerClaimJob` in `internal/web/handler_runner_jobs.go` to delegate to `RunService`.

## 3. Verification

- [x] 3.1 Add unit tests in `internal/service` asserting that retried trace appends do not trigger duplicate SSE broadcasts.
- [x] 3.2 Add integration tests verifying remote DAG nodes transition to `running` on claim.
- [x] 3.3 Verify full test suite passes with `make test` and `make test-coverage`.

## Workflow follow-up

- Archive change via `openspec archive remote-runner-trace-dedup -y` upon implementation.
