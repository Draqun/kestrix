# Proposal: cross-run-manual-gate-approval

## Why

In pipelines where a subset of tasks is triggered (e.g., path filtering, webhook partial runs, or manual single-task runs), a manual gate may depend on a task that was not included in the current run's execution graph. Currently, `isManualWaiting` in the UI and `ApproveManualTask` in `internal/service/service.go` strictly assert that every dependency listed in `task.DependsOn` exists within `run.task_results` with status `passed`. If a dependency was evaluated in an earlier run or excluded from the current run, the gate can never be approved and remains permanently stuck.

## What Changes

- Modify `ApproveManualTask` to resolve dependencies against the latest successful task results across the project's historical runs when a dependency is absent from the current run record.
- Update UI helper `isManualWaiting` in `DAGViewer.jsx` and `Pipelines.jsx` to recognize dependencies satisfied in parent/prior runs.
- Reject approval only if an upstream dependency is known to have failed or is actively pending/running elsewhere.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `fix-pipeline-manual-status`: permits approval of manual gates whose declared dependencies were satisfied in preceding runs.

## Non-goals

- Altering DAG cycle detection or pipeline parsing rules.
- Allowing approval when an upstream dependency in the current run has failed.

## Impact

- Modifies dependency evaluation in `internal/service/service.go` (`ApproveManualTask`).
- Updates `ui/src/components/DAGViewer.jsx` and `Pipelines.jsx`.
