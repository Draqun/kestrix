# Tasks: cross-run-manual-gate-approval

## 1. Backend Historical Dependency Resolution

- [x] 1.1 In `internal/store/pgstore_run.go`, add `FindLatestTaskStatus(projectName, ref, taskName string) (TaskStatus, string, error)` to find the most recent result and commit SHA for a task; verify with an integration test.
- [x] 1.2 In `internal/service/service.go` (`ApproveManualTask`), use `FindLatestTaskStatus` when a dependency is missing from the current run; verify with a unit test that approval succeeds if the dependency passed at the same commit SHA.

## 2. Frontend Gate State Calculation

- [x] 2.1 In `ui/src/components/DAGViewer.jsx`, update `isManualWaiting` to allow approval when parent nodes have passed in previous executions; verify component test passes.
- [x] 2.2 Run full test suite `make test` and UI tests `make ui-test` to ensure no regressions.

## Workflow follow-up

- Archive change via `openspec archive cross-run-manual-gate-approval -y` upon implementation.
