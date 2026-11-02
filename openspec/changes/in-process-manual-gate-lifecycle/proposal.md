# Proposal: in-process-manual-gate-lifecycle

## Why

In `local` and `docker` (in-process) runner modes, two lifecycle defects exist around manual gates:
1. Downstream tasks depending on a waiting manual gate start immediately instead of waiting (tested with `taskLoop` on `build` → `deploy` (manual) → `notify`: `deploy` becomes `manual`, but `notify` runs immediately and passes). In `remote` mode, the walker correctly blocks dependent tasks.
2. A failed upstream task leaves a run with a downstream manual gate in `running` status indefinitely (`runStatus` for `build failed` + `deploy manual` returns `running`). In `remote` mode, such runs properly terminate as `failed` with the manual gate marked `skipped`.

These gaps cause premature task execution and hung pipeline runs on host environments.

## What Changes

- In `internal/runner/runner.go` (`taskLoop`), check if any dependency of an eligible task is in `manual` or `pending` state before launching it. If an upstream dependency is `manual`, the downstream task remains `pending`.
- When an upstream task fails, mark downstream manual gates as `skipped` with skip reason pointing to the failed dependency, and allow `runStatus` to finalize the run as `failed`.
- Align in-process execution invariants with `remote` runner behavior.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `fix-pipeline-manual-status`: defines waiting requirements for downstream dependents of manual gates and skip transitions upon upstream failure in in-process pipelines.

## Non-goals

- Altering the approval API endpoint (`POST /api/projects/{name}/runs/{id}/tasks/{task}/run`).
- Modifying remote runner dispatch logic (which already behaves correctly).

## Impact

- Fixes execution order in `internal/runner/runner.go`.
- Ensures accurate terminal run state calculation in `internal/domain` and `internal/service`.
