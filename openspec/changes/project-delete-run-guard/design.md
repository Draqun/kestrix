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

**D1: Add `func (s *RunService) ActiveRunIDs() []int`, a mutex-guarded
snapshot of `s.active`'s keys, and have `handleDeleteProject` cross-reference
each ID's `ProjectName` via `runStore.GetRun` before calling `Delete`.**
Alternative considered: track project name alongside the cancel func in
`s.active` (`map[int]activeRun{cancel, projectName}`), avoiding the
`GetRun` round trip per ID. Rejected for this change — `s.active` is
typically small (concurrent active runs, not all runs ever), so the extra
lookups are cheap, and not changing `s.active`'s value type keeps this
change isolated from `cancel-kills-task-process`, which touches the same
map's population. A future change can fold them if the lookup cost matters.

**D2: Return `409 Conflict` (not `423 Locked` or `400`) naming the blocking
run ID(s), matching the existing `handleApproveManualTask`/`handleCancelRun`
convention of a body that names the specific resource in conflict.**

## Risks / Trade-offs

- [TOCTOU: a run could start on project X between the `ActiveRunIDs` check
  and the `Delete` call.] → Accept it. `Start` requires the project to still
  exist (it loads the project's pipeline first), so the race narrows to
  "delete succeeds, then a request to start a run 404s because the project
  is gone" — not "a run's rows vanish out from under it," which is the
  actual bug this change closes. Closing the race fully would need a
  DB-level lock across both operations, disproportionate to the risk.

## Migration Plan

No schema change; `ON DELETE CASCADE` stays as-is for the no-active-run
case. No data migration. Rollback is a plain revert.
