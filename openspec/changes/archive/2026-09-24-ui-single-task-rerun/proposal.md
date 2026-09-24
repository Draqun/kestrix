## Why

`POST /api/projects/{name}/tasks/{task}/run` has existed since the runner was
built, and `docs/` documents it, but nothing in the UI calls it: `ui/src/api/client.js`
has no wrapper, so the only way to re-run one task after fixing it is `curl`,
Swagger, or re-running the whole pipeline. On a pipeline whose slow task is the
one that failed, that is the difference between seconds and a full rebuild.

The endpoint is also the only run route that behaves differently from the rest:
`handleRunTask` (`internal/web/handler_run.go`) executes the task inline and
returns only `project`, `task`, `status`, `output`. It never returns the
`run_id` it just created, and since `RunService.RunTask` acquires a concurrency
slot, a re-run started while both slots are busy holds the HTTP request open
for as long as the queue takes. A UI button on top of today's response would
hang and then land the user nowhere.

## What Changes

- `POST /api/projects/{name}/tasks/{task}/run` returns immediately with
  `{"run_id": <id>, "project": ..., "task": ..., "status": "running"|"queued"}`
  and executes the task in the background, matching
  `POST /api/projects/{name}/run`.
- **BREAKING** for any existing caller of that route: the response no longer
  carries the finished `status` or the task `output`. Both are read from
  `GET /api/projects/{name}/runs/{runID}` once the run finishes.
- The run detail view gains a **Re-run** action on each terminal task in the
  job list and in the selected-job header, next to the existing manual-approval
  **Run** button, visible only to operator and above.
- Re-running navigates to the new run, so the user follows the fresh run rather
  than watching a stale one.
- `ui/src/api/client.js` gains `runTask(name, task)`.

## Capabilities

### New Capabilities
- `ui-single-task-rerun`: re-running one task of a finished run from the run
  detail view — where the action appears, who sees it, and what the user lands
  on afterwards.

### Modified Capabilities
- `run-scheduling`: the "Concurrent run limit" requirement already says a
  single-task re-run occupies a slot; it gains the acceptance behavior that the
  route returns its run id and status before the slot is acquired, so a queued
  re-run is visible rather than pending on an open HTTP request.

## Non-goals

- No re-run of a whole failed run, no "re-run failed tasks only", no re-run
  with edited variables.
- No re-run from the dashboard, the project list, or the DAG node itself; the
  run detail view is the only entry point.
- No change to manual approval (`POST .../runs/{runID}/tasks/{task}/run`),
  which stays a separate route with its own button.
- No re-run of a task while its own run is still `running` or `queued`.

## Impact

`internal/web/handler_run.go` (response shape, background execution),
`ui/src/api/client.js`, `ui/src/pages/Pipelines.jsx` (run detail view),
`docs/` and the Swagger annotations for the changed response. No database
migration, no config setting.
