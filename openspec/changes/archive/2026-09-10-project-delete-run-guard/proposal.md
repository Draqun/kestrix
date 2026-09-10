## Why

`handleDeleteProject` (`internal/web/handler_project.go:215-222`) calls
`projectService.Delete` unconditionally, and `runs.project_name` is declared
`REFERENCES projects(name) ON DELETE CASCADE` (`internal/store/pgstore.go:94`).
Deleting a project while one of its runs is executing removes that run's row
(and its task results) instantly, while the in-memory `RunService` goroutine
for that run — still tracked in `active`/`done`
(`internal/service/service.go`) — keeps executing and later calls
`FinishRun`/`AppendTaskResult`/`FlushRunOutput`
(`internal/store/pgstore_run.go`), each an `UPDATE ... WHERE id = $N` whose
`RowsAffected` is never checked. The update silently no-ops: the run finishes
against a row that no longer exists, its result is lost, and there is no
longer any way to see or cancel it (`GetRun` already 404s the moment the
project is deleted). This is a pre-existing gap in project lifecycle
management, surfaced while auditing the run-attribution and multi-source
changes that touch the same delete path without closing it.

## What Changes

- `handleDeleteProject` (or `ProjectService.Delete`) checks for runs still in
  `RunService`'s active set before deleting a project, and refuses with a
  `409 Conflict` naming the in-progress run(s) instead of deleting silently.
- The error response tells the caller to cancel the run first, matching the
  existing `POST /api/projects/{name}/runs/{runID}/cancel` route.

## Non-goals

- Cascading a cancel automatically when a project is deleted — the operator
  decides whether to cancel or wait; this change only prevents silent data
  loss, it does not add an auto-cancel-on-delete behavior.
- Changing `ON DELETE CASCADE` semantics for finished runs; a project with no
  active runs still deletes (and cascades) exactly as today.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `project-registration`: the project-deletion requirement must refuse to
  delete a project with an actively-running run instead of deleting it out
  from under the run.

## Impact

- `internal/web/handler_project.go`: `handleDeleteProject` gains an
  active-run check before calling `Delete`.
- `internal/service/service.go`: exposes a way to ask "does project X have
  an active run" from the `active`/`done` bookkeeping `RunService` already
  keeps (currently keyed by run ID, not project name).
- No schema change; the existing `ON DELETE CASCADE` stays as the backstop
  for projects with no active run.
