# Tasks: in-process-manual-gate-lifecycle

## 1. Dependency Readiness in In-Process Execution

- [x] 1.1 In `internal/runner/runner.go` (`taskLoop`), update the dependency satisfaction check so that a task whose dependency has status `manual` or `pending` is not dispatched; verify with a unit test that in `build` → `deploy` (manual) → `notify`, `notify` stays `pending` while `deploy` is `manual`.
- [x] 1.2 In `internal/runner/runner.go`, when an upstream task fails, mark downstream manual tasks as `skipped` with skip reason referencing the failed task; verify with a unit test that `build` (failed) + `deploy` (manual) results in `deploy` having status `skipped`.

## 2. Terminal Run Status Alignment

- [x] 2.1 In `internal/runner` and `internal/domain`, verify that `CalculateRunStatus` returns `failed` when an upstream task fails and its downstream manual gate is skipped; verify with a test case covering mixed failed and manual tasks.
- [x] 2.2 Run full test suite `go test ./internal/runner/... ./internal/service/...` to ensure all in-process pipeline tests pass with 100% statement coverage.

## Workflow follow-up

- Archive change via `openspec archive in-process-manual-gate-lifecycle -y` upon implementation.
