# Design: task-rerun-original-revision

## Context

When `POST /api/projects/{name}/tasks/{task}/run` is called today, it accepts only the project name and task name. The handler acquires the repository by branch name, pulling the latest commit. In `internal/store/pgstore_run.go`, runs record their acquired sources in `runs.sources` as a JSONB array including `resolved_commit` and `requested_ref`.

## Goals / Non-Goals

**Goals:**
- Enable the re-run endpoint to accept an optional `from_run_id` parameter.
- When `from_run_id` is supplied, extract the exact commit SHA for each configured repository and pass them as initial sources to `StartTask`.
- Centralize `CreateRun` and run-record initialization within `StartTask`.

**Non-Goals:**
- Supporting re-runs when the original run has empty source records.

## Decisions

### D1: Accept `from_run_id` in `handleRunTask`
- **Choice:** Accept `from_run_id` as a query parameter in `POST /api/projects/{name}/tasks/{task}/run?from_run_id=123` or JSON body. The UI (`client.js` `rerunTask`) passes the ID of the run currently being viewed.
- **Alternatives considered:** Making `from_run_id` mandatory. Rejected to maintain backward compatibility with direct API/CLI scripts that want to run a task on HEAD.

### D2: Move run creation into `StartTask`
- **Choice:** Align `StartTask` with `Start` so that `RunService` creates the run record, sets initial sources atomically, and returns `domain.Run`.
- **Alternatives considered:** Keeping handler-managed `CreateRun`. Rejected because handling it in the service avoids duplicated error handling and state races.

## Risks / Trade-offs

- **[Risk]** The original commit might have been force-pushed away or garbage-collected in the upstream remote.  
  → *Mitigation:* Fail clearly with `ErrCommitNotFound` during checkout if git fetch fails to locate the SHA.
