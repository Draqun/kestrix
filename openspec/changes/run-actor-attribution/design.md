## Context

See proposal.md — Why. The constraints that shape this design:

- `internal/store/pgstore.go:91-103` creates `runs` with no actor column;
  `pgstore.go:144-155` is the idempotent `statements` slice where every later
  column has been added as `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`. There is
  no migration tool.
- `task_results` is a JSONB append log of `domain.TaskResult`
  (`internal/domain/types.go:162-168`); readers take the *last* entry per task
  name (`AGENTS.md`, `pgstore_run.go:171-186`). Anything stamped on a task must
  therefore ride on every later entry for that task or it disappears from the
  "latest" view.
- The actor is available only in the HTTP layer: `claimsFromContext`
  (`internal/web/middleware.go:29`) yields `domain.Claims.UserName` after
  `RequireAuth`. Handlers hand the service `context.Background()`
  (`handler_run.go:56`, `:283`), so the actor cannot travel on the context.
- Start goes through `RunService.Start` (`service.go:164`); approval through
  `RunService.ApproveManualTask` → `RunTask` (`service.go:393`, `:293`);
  cancel bypasses the service and calls `runStore.CancelRun` directly
  (`handler_run.go:305`).
- The webhook path dispatches to GitLab and never calls `CreateRun`; the CLI
  `kestrix run` has no store. Every run row today is user-initiated.
- `scripts/test.sh:67` holds `internal/web` at 100% and `internal/service` at
  100%; `internal/domain` ≥ 90%. Every branch added here needs a test.
- The UI reads runs raw (`run.started_at`, `run.task_results`) with no
  normalisation layer (`ui/src/api/client.js:231-250`).

## Goals / Non-Goals

**Goals:**

- Every human-initiated run state change names its actor, stored beside the
  event it describes, and readable from the same endpoints that already return
  the run.
- Attribution is decided by the server from authenticated identity. No client
  input reaches it.
- No new table, dependency, or migration step. Two columns and two JSON fields.

**Non-Goals:**

- A queryable audit trail (filter runs by actor, list an actor's actions).
- Threading actors through the runner. The runner executes; the service
  records.
- Changing what the dashboard shows.

## Architecture

```
  request ─▶ RequireAuth ─▶ RequireRole(operator) ─▶ handler
                │
                └─ Claims.UserName ──▶ actor = "user:" + name
                                          │
     ┌────────────────────────────────────┼─────────────────────────────┐
     │ start                              │ approve                     │ cancel
     ▼                                    ▼                             ▼
 RunService.Start(opts.Actor)   RunService.ApproveManualTask(opts.Actor)  RunService.Cancel(id, actor)
     │                                    │                                 │
     │ CreateRun(..., startedBy)          │ ApplyTaskResult(                 │ CancelRun(id, canceledBy)
     │                                    │   {Status: running,             │
     │                                    │    ApprovedBy, ApprovedAt})      │
     │                                    │   …later entries copy both       │
     ▼                                    ▼                                 ▼
 runs.started_by                 runs.task_results[*].approved_by   runs.canceled_by
                                                    .approved_at
     │                                    │                                 │
     └──────── log "run: started|approved|canceled actor= project= run= [task=]"

 GET …/runs/{id}, GET …/runs, GET /api/runs  ──▶ Run{StartedBy, CanceledBy, TaskResults[…]}
                                               ──▶ Pipelines.jsx: "by <actor>" / "unknown"
```

## Decisions

### D1. Actor is an opaque `<kind>:<id>` string, not a user id

`runs.started_by TEXT NOT NULL DEFAULT ''`, `runs.canceled_by TEXT NOT NULL
DEFAULT ''`, and `TaskResult.ApprovedBy string`. The value is `user:<name>`.

Alternative considered: a nullable foreign key to `users.id`. Rejected —
`handler_user.go` deletes accounts outright (rbac-hardening task 1.3
deviation), and `ON DELETE SET NULL` would erase exactly the record an audit
needs; `ON DELETE RESTRICT` would make account deletion depend on run history.
A copied name is the record.

Alternative considered: a bare account name. Rejected on cost, not need: the
prefix is free now and saves a rewrite when a webhook-created run needs
`webhook:<project id>`. Only `user:` is emitted; the spec reserves the others
and forbids inventing more here.

### D2. The actor travels on `RunnerOptions.Actor`, not on the context

Handlers already build a `domain.RunnerOptions` for `Start` and
`ApproveManualTask` and pass `context.Background()` so the run outlives the
request. Adding `Actor string` to `RunnerOptions` reuses the one struct that
already crosses the handler→service boundary for both paths. The runner
ignores the field.

Alternative considered: a context value. Rejected — the handlers deliberately
drop the request context, and a context key is invisible in the signature,
which is how the current attribution gap arose.

### D3. Cancel moves into the service

`handleCancelRun` calls `runStore.CancelRun` directly. This change adds
`RunService.Cancel(runID int, actor string) error` that calls
`CancelRun(id, canceledBy)` and logs, and the handler switches to it. Three
actions, one place that stamps and logs, one mock to assert on.

`RunStore.CancelRun` gains the actor argument rather than a second
`SetCanceledBy` call: the store already decides inside one query whether the
run is `running`, and the actor must be written in that same statement so a
cancel of a finished run records nothing (spec: "Cancel of a finished run
records nothing").

### D4. Approver rides on every subsequent result entry for the task

`ApproveManualTask` → `RunTask` writes a `running` entry immediately
(`service.go:305`) and a terminal entry when done (`:336`). Readers take the
last entry per name, so `ApprovedBy`/`ApprovedAt` are set on the `running`
entry and copied onto the terminal entry inside `RunTask` from the `running`
one it wrote. `omitempty` on both keeps unapproved tasks' entries unchanged
byte-for-byte, so the `pipeline_def`/`task_results` history of existing runs
is untouched.

Alternative considered: a separate `approvals` JSONB column keyed by task.
Rejected — a second structure for the same task means two places to keep in
sync on every read that computes "latest".

`handleRunTask` (re-run a single task by name) shares `RunTask`; it passes
`opts.Actor` too, so a re-run of a manual task records who did it. That is
free, and the proposal's non-goal only rules out doing *more* for that route.

### D5. Actor is derived from claims in the handler, once

A small helper `actorFrom(r) string` in `internal/web` returns
`"user:" + claims.UserName` or `""` if no claims are on the context (only
reachable in tests, since `RequireAuth` guards every run route). The three
handlers call it. Nothing reads a body or header for it (spec: "Caller-supplied
actor is ignored").

### D6. Logging in the service, `log.Printf`, same shape as `authz:` lines

`run: started actor=%q project=%q run=%d`, `run: approved … task=%q`,
`run: canceled …`. The service already has no HTTP dependency and is the only
place all three actions pass through after D3. Same `key=value` shape as
`middleware.go:176-186` so one grep finds decisions and actions together.

## Security considerations

- The actor comes from `domain.Claims`, which `RequireAuth` builds from the
  stored account after signature verification (rbac-hardening D1). A request
  cannot name another account.
- Recording an actor does not widen who may act: `RequireRole(operator)`
  still gates all three routes. This change adds no route.
- Log lines carry the account name, project, run id, and task name only —
  never the token, body, or output (spec: "Log carries no secret").
- Attribution is deliberately not deleted with the account (D1). The trade is
  that a former account's name persists in run history; that is the purpose.

## Risks / Trade-offs

- [Existing runs show `unknown`] → Expected and stated in the spec; no
  backfill is possible because the information was never recorded.
- [`RunStore` port change ripples to `PGStore`, `MockRunStore`, and the
  compile-time assertions] → That is the point of the assertions
  (`hexagonal-ports`); the build fails until every adapter is updated.
- [Approver lost if a later entry is written by a path that does not copy it]
  → Only `RunTask` writes entries after approval (`service.go:305`, `:336`);
  the copy lives there. A test asserts the terminal entry still carries it.
- [Frontend test coverage] → `Pipelines.jsx` is outside the modules with
  enforced thresholds (`docs/fe.md`); one jsdom assertion each for "by alice"
  and "unknown" is still added so the rendering is not silently dropped.

## Migration Plan

1. Deploy. Startup runs the two `ADD COLUMN IF NOT EXISTS` statements inside
   the existing schema transaction; on failure the transaction rolls back and
   the process refuses to start, as today.
2. No data step. Old rows read as `''` and render `unknown`.
3. Rollback: the previous binary ignores the two extra columns and the two
   extra JSON fields (unknown JSON fields are dropped on unmarshal). No
   column removal is needed to roll back.
