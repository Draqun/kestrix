## Why

`rbac-auth` decides *whether* an account may start, approve, or cancel a run,
but nothing records *who did*. The `runs` table
(`internal/store/pgstore.go:91-103`) has `started_at` and `finished_at` and no
actor column; `domain.TaskResult` (`internal/domain/types.go:162-168`) has no
approver. An operator can approve a production deploy and the run detail shows
only that it happened. Delivery tooling that cannot answer "who approved this?"
is not auditable, and every later audit feature needs this record to exist
first.

## What Changes

- Record the actor on the three state changes a person can make to a run:
  `started_by` and `canceled_by` on the run, `approved_by` and `approved_at`
  on the manual task's result entry inside `task_results`.
- An actor is one opaque string of the form `<kind>:<id>`. Today the only
  kind Kestrix produces is `user:<account name>`; `webhook:` and `cli:` are
  reserved for the day a webhook or the CLI creates a run row (neither does
  now — the webhook dispatches to GitLab, `kestrix run` has no store).
- No foreign key to `users`. Accounts get deleted; the history must outlive
  them and keep the name that acted.
- Return the actor fields on every run response (`GET .../runs/{id}`,
  `GET .../runs`, `GET /api/runs`) and show them in the run detail and the
  runs list. A run recorded before this change shows `unknown`.
- Emit one log line per start, approval, and cancel with actor, project, and
  run id, in the style of the `rbac-hardening` decision logs.
- Schema grows by two idempotent `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`
  entries, following the existing `statements` slice. No migration tool, no
  backfill.

## Non-goals

- A durable audit log table, retention policy, or audit query API. This
  change is the minimal precursor: it records the actor where the event
  already lives. `rbac-hardening` keeps "audit log" as a named non-goal and so
  does this.
- Attributing runs to a webhook or to the CLI. Neither creates a run row.
- Dashboard "recent runs" columns; it links to the detail page.
- Anything about the actor beyond the recorded string (no link to the
  account, no role-at-time-of-action).

## Capabilities

### New Capabilities

- `run-attribution`: which actor started, approved, and canceled a run, how
  the actor is represented, how it survives account deletion, and how it is
  exposed and logged.

### Modified Capabilities

None. `hexagonal-ports` names the `RunStore` methods, not their signatures,
so widening `CreateRun` and `CancelRun` by one argument changes no
requirement. `service-layer` describes orchestration that stays the same.

## Impact

- `internal/store/pgstore.go`: two `ALTER TABLE` entries. `pgstore_run.go`:
  `CreateRun`, `CancelRun`, and the three `SELECT` lists gain the columns.
- `internal/domain/ports.go`: `Run` gains `StartedBy`/`CanceledBy`,
  `RunnerOptions` gains `Actor`, `RunStore.CreateRun`/`CancelRun` take the
  actor. `types.go`: `TaskResult` gains `ApprovedBy`/`ApprovedAt`.
  `internal/mocks/mocks.go` follows.
- `internal/web/handler_run.go`: three handlers read the actor from the
  request claims. `internal/service/service.go`: `Start`,
  `ApproveManualTask`, and a new `Cancel` stamp and log it.
- `ui/src/pages/Pipelines.jsx`: run detail header and runs list rows.
- `docs/swagger.*` regenerate from the struct tags. Coverage gates:
  `internal/web` and `internal/service` stay at 100%.
