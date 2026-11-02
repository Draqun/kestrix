# Design: cross-run-manual-gate-approval

## Context

`ApproveManualTask` currently checks `run.TaskResults` directly to ensure every task in `task.DependsOn` has `Status == TaskStatusPassed`. In partial or single-task executions, the run's `TaskResults` contains only the tasks triggered in that specific run. An unexecuted dependency has no entry in `run.TaskResults`, causing approval validation to return `ErrDependenciesNotPassed`.

## Goals / Non-Goals

**Goals:**
- Enable operators to approve manual gates in partial and single-task runs when dependencies passed in prior runs on the same branch.
- Prevent approval if the latest recorded outcome of an upstream dependency was `failed` or `canceled`.

**Non-Goals:**
- Crossing branch boundaries to find satisfied dependencies (dependencies must belong to the same ref/branch).

## Decisions

### D1: Fall back to latest task status lookup across runs
- **Choice:** If a required dependency is missing from the current run's `TaskResults`, query `RunStore` for the latest task result for that task name on the project and matching ref. If it passed, treat the dependency as satisfied.
- **Alternatives considered:** Copying historical task results into the new run at creation time. Rejected because that would falsely imply the task was executed as part of the current run.

## Risks / Trade-offs

- **[Risk]** Approving a deployment against stale build artifacts from long ago.  
  → *Mitigation:* Require that the prior run's checkout SHA matches the current run's primary source SHA.
