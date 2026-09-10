## 1. RunOne gains process-group termination

- [x] 1.1 In `internal/runner/runner.go`, call `setCancelViaProcessGroup(cmd, grace)` in `RunOne` immediately before `cmd.Run()`, reading `grace` from `config.Current()` the same way `LocalExecutor.RunTask`/`GitLabRunnerExecutor.RunTask` already do; verify a unit test using the `commandContext` stub asserts `cmd.SysProcAttr.Setpgid` is set and `cmd.Cancel`/`cmd.WaitDelay` are non-nil after `RunOne` builds its command, matching the existing assertions for `LocalExecutor`.
- [x] 1.2 Verify `internal/runner`'s existing "cancel kills the process group" integration test (build-tag/`testing.Short()`-guarded, per `runner-unit-only-coverage`) gains a case that cancels a context mid-`RunOne` and asserts the spawned process is gone within grace plus two seconds, mirroring the equivalent DAG-executor test.

## 2. RunService registers RunTask/ApproveManualTask like Start

- [x] 2.1 In `internal/service/service.go`, add a `context.WithCancel(ctx)` wrap at the top of `RunTask`, register the resulting `cancel` and a `done chan struct{}` in `s.active`/`s.done` keyed by `runID` before calling `s.taskRunner.RunTask`, and `defer` both the `close(done)` and the map deletions so they run even if `s.taskRunner.RunTask` panics or returns early; verify a service unit test asserts the run is present in `s.active` while a blocking fake `taskRunner` is executing and absent immediately after it returns.
- [x] 2.2 Apply the identical registration to `ApproveManualTask`; verify a service unit test covers the same present-during / absent-after assertion for it.
- [x] 2.3 Verify a service unit test calls `Cancel` while a fake blocking `taskRunner.RunTask` (started via `RunTask` or `ApproveManualTask`) is executing and asserts: the fake's context is canceled, `Cancel` returns only after the fake returns (or times out per `CancelGrace`+`CancelWaitBuffer` if the fake ignores cancellation), and `runStore.CancelRun` is called exactly once after the fake returns — reusing the existing `blockingCancelRunner` test helper pattern from the `Start`/DAG cancel tests.

## 3. Spec conformance

- [x] 3.1 Verify `internal/web` route-level tests (`handleRunTask`, `handleApproveManualTask`, `handleCancelRun`) still pass unchanged — this change alters only what `RunService` does internally, not the HTTP contract.
- [x] 3.2 Run `go test ./internal/runner/... ./internal/service/...` with `-race` and confirm both packages remain at 100% unit-only statement coverage (`make runner-unit-coverage`, `make test-coverage`).
