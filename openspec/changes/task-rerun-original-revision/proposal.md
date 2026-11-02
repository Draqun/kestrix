# Proposal: task-rerun-original-revision

## Why

In MR !52, single-task re-run was introduced (`POST /api/projects/{name}/tasks/{task}/run`). However, two architectural details were deferred:
1. Revision pinning: The re-run currently acquires sources at the branch HEAD rather than the exact commit SHA of the original run. If new commits landed on the branch since the original run, the task runs against modified code, invalidating debugging and regression diagnosis.
2. Run record ownership: `StartTask` currently splits run creation and scheduling between the HTTP handler and service layer, unlike `RunService.Start` which owns the run record lifecycle end-to-end.

## What Changes

- Pass the original run's ID or source revision snapshot to `POST /api/projects/{name}/tasks/{task}/run` (e.g. via optional query parameter or body).
- In `RunService.StartTask`, pin source acquisition to the exact commit SHA recorded in the original run's sources.
- Fully encapsulate run creation and state persistence inside `RunService.StartTask`, removing duplicate setup logic from the HTTP handler.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `ui-single-task-rerun`: requires single-task re-run to execute against the original run's recorded source revision.

## Non-goals

- Re-running entire multi-task pipelines at a historical revision (covered by standard project re-run).
- Allowing arbitrary variable edits during re-run.

## Impact

- Updates `internal/service/service.go` (`StartTask`), `internal/web/handler_run.go`, and `ui/src/api/client.js`.
