## Context

See proposal.md — Why.

Two shapes exist side by side for starting work today:

- `RunService.Start` (`internal/service/service.go:513`) creates the run,
  decides `queued` vs `running` synchronously through `markQueuedIfBusy`, then
  returns the run id and executes in a goroutine. `handleRunProject` returns
  that id and status right away.
- `RunService.RunTask` (`internal/service/service.go:809`) tracks the run,
  calls `acquireSlot` — which blocks while the queue is full — and returns the
  finished `TaskResult`. `handleRunTask` (`internal/web/handler_run.go`) calls
  it inline and encodes the finished status and output. The run id it created
  never leaves the handler.

`RunTask` also owns workspace cleanup through `opts.WorkspaceCleanup` (invoked
by `runTaskInternal`) and secret resolution through `service.BuildVarStore`,
both of which currently happen while the HTTP request is open.

The UI's run detail view (`ui/src/pages/Pipelines.jsx`) already renders a per-task
action for manual approval, gated on `canOperate` (`roleAtLeast('operator')`)
and `isManualWaiting`.

```
  before                                   after

  POST .../tasks/{task}/run                POST .../tasks/{task}/run
      |                                        |
      | BuildVarStore                          | BuildVarStore
      | CreateRun                              | CreateRun
      | RunTask ── blocks on slot ──┐          | markQueuedIfBusy (sync)
      |            executes task    |          |
      | FinishRun                   |          +--> 202-style JSON:
      |                             |          |    {run_id, status}
      +--> JSON {status, output} <--+          |
                                               `-> goroutine: acquire slot,
                                                   execute, FinishRun
```

## Goals / Non-Goals

**Goals:**

- One acceptance contract for every route that starts a run: id plus accepted
  status, never a finished verdict.
- Reuse the scheduling halves (`markQueuedIfBusy`, `waitAndStart`) that
  `Start` already uses, rather than adding a third scheduling path.
- Keep the cancel-after-finish rule that `handleRunTask` already encodes.

**Non-Goals:**

- Unifying `Start` and `RunTask` into one function. They differ in what they
  execute (whole DAG vs one task); only the scheduling and response halves are
  shared.
- Streaming task output to the UI. The run detail view already polls.

## Decisions

### D1: Split `RunTask` into an accepting half and a background half

Add `RunService.StartTask(ctx, runID, projectName, proj, taskName, opts) (queued bool, err error)`
that mirrors `Start`: track the run, call `markQueuedIfBusy` synchronously, then
run `waitAndStart` + `runTaskInternal` + the finish bookkeeping in a goroutine.
`RunTask` stays for callers that want the synchronous result (tests, and
`ApproveManualTask`, which is out of scope here).

**Alternative considered:** keep the handler synchronous and add a client-side
timeout. Rejected: a queued re-run would still hold a connection for an
unbounded time, and the user gets no run to look at meanwhile.

### D2: Finish bookkeeping moves into the service, not the handler

The `FinishRun` call, the "task finished after cancel" check, and the
`WorkspaceCleanup` callback move with the background half. The handler cannot
keep them: it has already responded. The cancel check keeps its current
semantics — a run already marked `canceled` is not overwritten.

### D3: The response is the breaking part, and it is the point

`{"run_id", "project", "task", "status"}` replaces `{"project", "task",
"status", "output"}`. `status` now means accepted state (`running` or
`queued`), not verdict. Swagger annotations and `docs/` change with it. The
route is documented but has no known UI or script consumer in this repo, and
the CHANGELOG entry marks it **BREAKING** for anyone outside it.

**Alternative considered:** a new route (`POST .../tasks/{task}/start`) leaving
the old one intact. Rejected: two routes doing the same thing with different
contracts is the more expensive mistake, and the old contract is the one that
cannot support a UI button.

### D4: UI navigates by run id, reusing the existing run detail route

`handleRerun` calls `runTask(projectName, task)`, then switches the view to the
returned `run_id` through the same state the run list already uses to open a
run. No new page, no new polling loop: the detail view's existing poll picks up
`queued` → `running` → terminal.

### D5: Action visibility is computed from the task, not the run

A re-run button is rendered for a task when the run is terminal and the task
has a terminal status of its own. `isManualWaiting` keeps owning the approval
button, so the two never render for the same task.

## Risks / Trade-offs

- **An external script parses `output` from the response** → **BREAKING** in
  the changelog with the replacement named (`GET /api/projects/{name}/runs/{runID}`).
- **Moving `FinishRun` into a goroutine loses the error path the handler logged**
  → the background half logs the same warnings; a unit test asserts the run
  record reaches a terminal status when `RunTask`'s internals fail.
- **Workspace cleanup now runs after the response, so a failure is invisible to
  the caller** → cleanup already ran through `defer` inside `runTaskInternal`;
  only the observer changes, and the failure is logged.
- **A user re-runs the same task repeatedly and fills the queue** → each re-run
  is an ordinary run under `max_concurrent_runs`; the queue bounds it, and the
  button is disabled while its own request is in flight.
- **100% unit coverage gate on a new goroutine path** → the background half is
  a named method on `RunService`, called directly in tests, not an anonymous
  closure.

## Migration Plan

1. Ship the service split and the handler change together; the route keeps its
   path and method.
2. No database change, no setting, no data migration.
3. Rollback is the previous image; a rolled-back instance serves the old
   synchronous contract again.
