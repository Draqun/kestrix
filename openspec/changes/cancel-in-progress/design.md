# Design: cancel-in-progress for superseded pipeline runs

## Context

See `proposal.md` for motivation. Currently, `RunService.Start` creates a run record and either executes it in-process (`local`/`docker`) with a concurrency slot or dispatches remote jobs (`remote`). In-process runs beyond `runner.max_concurrent_runs` enter status `queued` in FIFO order.

`RunService.Cancel(runID, canceledBy)` already provides full cancellation semantics across all execution modes:
- `local`: sends `SIGTERM` to the process group, then `SIGKILL` after `runner.cancel_grace`.
- `docker`: stops the container.
- `remote`: marks unclaimed `pending` jobs unclaimable and prevents reclaim.
- `queued`: transitions directly to `canceled` without starting or holding a concurrency slot.

What is missing is automatic invocation of `Cancel` when a newer run arrives on the same ref.

```
Webhook Push / Manual Whole-Pipeline Trigger
           │
           ▼
┌────────────────────────────────────────────────────────┐
│  Resolve Pipeline & Primary Source Ref (e.g. "feat/x") │
└──────────────────────────┬─────────────────────────────┘
                           │
                           ▼
┌────────────────────────────────────────────────────────┐
│                  RunService.Start                      │
│                                                        │
│  Is proj.CancelInProgress == true?                     │
│  ├── No  ──▶ Proceed to acquire slot & createRun       │
│  │                                                     │
│  └── Yes ──▶ FindActiveRunsByRef(project, "feat/x")    │
│                     │                                  │
│                     ▼                                  │
│         Cancel superseded runs in parallel             │
│         with bounded grace wait                        │
│                     │                                  │
│                     ▼                                  │
│         Proceed to acquire slot & createRun            │
└────────────────────────────────────────────────────────┘
```

## Goals / Non-Goals

**Goals:**
- Declarative `cancel_in_progress` opt-in on the HCL `project` block.
- Zero wasted runner computation on superseded commits for the same branch.
- Re-use existing atomic status transitions and process termination in `RunService.Cancel`.
- Auditability via distinct `canceled_by: "system:cancel-in-progress"`.

**Non-Goals:**
- Custom grouping keys or arbitrary expressions (e.g. GitHub Actions `concurrency.group`).
- Canceling runs across differing branches or projects.
- Auto-canceling single-task re-runs.

## Decisions

- **D1: Configured via HCL `project` attribute `cancel_in_progress`.**
  - *Rationale:* Kestrix pipelines are declared in HCL as code. Placing `cancel_in_progress = true` in the `project` block keeps the policy version-controlled alongside repository code.
  - *Alternative:* Storing an instance-wide or project-table setting. Rejected: different branches or repositories within the same instance require different policies.

- **D2: Match active runs by `(project_name, primary_ref)`.**
  - *Rationale:* Pipeline runs belong to a project and are triggered against a specific branch or tag (recorded on the primary `AcquiredSource.RequestedRef`). Matching both ensures a push to `feature-b` never cancels an ongoing run on `main`.
  - *Alternative:* Matching only `project_name`. Rejected: would kill concurrent builds of unrelated branches.

- **D3: Recorded cancellation actor `system:cancel-in-progress`.**
  - *Rationale:* `run-attribution` stores `canceled_by` for audit logs and UI display. A dedicated system prefix clarifies that the cancellation was automated, not user-triggered.

- **D4: RunService.Start owns policy check and cancels prior runs before slot acquisition.**
  - *Rationale:* Placing policy evaluation inside `RunService.Start` (via `cancelSupersededRuns`) guarantees uniform enforcement across all trigger origins (webhook pushes, manual runs via API) that provide initial sources with a primary branch ref. Canceling queued or running superseded runs in parallel before acquiring runner slots immediately frees concurrency slots for the new run.
  - *Failure Ordering:* Cancellation signals are dispatched before `createRun`. If `createRun` fails subsequently (e.g. store error), the superseded runs have already been canceled, prioritizing safety against competing concurrent work. Lookup or cancel errors are logged as warnings and do not abort starting the new run.

- **D5: Store query `FindActiveRunsByRef`.**
  - *Rationale:* `PGStore` indexes runs by `project_name` and `status`. Adding a targeted query for non-terminal statuses (`running`, `queued`) filtering on the primary source ref avoids loading full run records into memory.

## Security Considerations

- Automatic cancellation only affects runs within the exact same project and ref.
- Secrets and redaction rules remain intact during cancellation; `Cancel` preserves existing log redaction.
- Actor attribution `system:cancel-in-progress` prevents spoofing a user identity.

## Risks / Trade-offs

- [A rapid flurry of pushes cancels runs before any tests finish] → Standard CI behavior; desired when only the latest commit matters. If a project wants all commits tested, it omits `cancel_in_progress`.
- [Database query latency during webhook delivery] → `FindActiveRunsByRef` filters on indexed columns (`project_name`, `status`) and matches the primary source JSONB ref.

## Migration Plan

- Fully backward compatible.
- Default value is `false`, preserving current FIFO execution for all existing projects.
