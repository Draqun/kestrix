## Context

See proposal.md — Why.

```
DELETE /api/projects/{name}
        │
        ▼
handleDeleteProject ──► projectService.Delete(name) ──► DB: ON DELETE CASCADE
        │                                                  (runs, task_results gone)
        │
        └── (meanwhile, unaffected) RunService goroutine for an active run
                 of {name} keeps calling FinishRun/AppendTaskResult/
                 FlushRunOutput against a run ID that no longer exists —
                 UPDATE ... WHERE id = $N affects 0 rows, silently.
```

`RunService.active` (`internal/service/service.go`, populated by `Start`,
and — once `cancel-kills-task-process` lands — by `RunTask`/
`ApproveManualTask` too) is keyed by `runID`, not project name; there is no
existing way to ask "does project X have a run in flight" without a new
accessor.

## Goals / Non-Goals

**Goals:** deletion of a project with an active run is refused, not silently
data-losing.

**Non-Goals:**
- Auto-cancelling the active run on delete (see proposal.md — Non-goals).
- Guarding against a run that starts in the race window between the check
  and the delete transaction — see Risks below.

## Decisions

**D1: Track the project name alongside the cancel func in `RunService.active`
(`map[int]activeRun{cancel, project}`), and have `handleDeleteProject` call
the new `ActiveRunIDsForProject(name)` accessor before calling `Delete`.**
No run-row lookup is involved, so the guard needs no store round trip per
ID, cannot fail open on a transient store error, and never pulls the
accumulating output log just to compare a project string. `Start` records
its `projectName`; `RunTask`/`ApproveManualTask` record theirs via
`trackActive`. `ActiveRunIDsForProject` is the only accessor — an unscoped
`ActiveRunIDs` snapshot would have no caller. This revisits the alternative
rejected below: review feedback showed
the `GetRun`-per-ID variant both loaded the full row (including `output`,
the largest and still-growing column) and treated a lookup failure as "not
this project", i.e. fail-open into the exact data loss this change prevents.

Alternative considered and rejected: keep `s.active` as
`map[int]context.CancelFunc` and cross-reference each ID's `ProjectName`
via `runStore.GetRun`. Rejected — the per-ID lookups pull the whole run
row (pipeline snapshot plus output log) for a string comparison, and any
lookup error degrades to skipping the run, which fails open into deletion.

**D2: Return `409 Conflict` (not `423 Locked` or `400`) naming the blocking
run ID(s), matching the existing `handleApproveManualTask`/`handleCancelRun`
convention of a body that names the specific resource in conflict.**

## Risks / Trade-offs

- [TOCTOU: a run could start on project X between the `ActiveRunIDsForProject` check
  and the `Delete` call.] → Accept it. `Start` requires the project to still
  exist (it loads the project's pipeline first), so the race narrows to
  "delete succeeds, then a request to start a run 404s because the project
  is gone" — not "a run's rows vanish out from under it," which is the
  actual bug this change closes. Closing the race fully would need a
  DB-level lock across both operations, disproportionate to the risk.
- [A run waiting for manual approval is not in `active`, so it does not
  block the delete.] → Accept it. `active` holds only runs with a live
  goroutine; a run paused at a manual gate has none (see `Cancel`'s doc
  comment, which lists the same case). Deleting the project cascades that
  run's rows away and the later approval request answers `404` at
  `handleApproveManualTask`, which loads the run before doing anything —
  so no write lands on a deleted row, and the silent data loss this change
  closes does not reappear. Blocking on waiting runs too would mean
  querying the store for unfinished runs, which the `Non-Goals` above
  deliberately keep out of this change.

## Migration Plan

No schema change; `ON DELETE CASCADE` stays as-is for the no-active-run
case. No data migration. Rollback is a plain revert.
