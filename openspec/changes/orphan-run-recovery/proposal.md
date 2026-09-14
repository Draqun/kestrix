## Why

Run execution lives in goroutines inside the `serve` process (`RunService.Start`, `internal/service/service.go:503`). When the process restarts (upgrade, crash, `docker compose up -d`), every run that was executing is left in the `runs` table with `status='running'` forever: `cmdServe` never looks at the table on startup, and `Cancel` only documents that such a run "is canceled in the store directly, with no termination attempted" if someone happens to cancel it (`service.go:543`). A customer who redeploys during an evaluation sees runs spinning indefinitely in the UI, and the project-delete guard, dashboard counters and `GET /api/runs?status=running` all report ghosts.

## What Changes

- On startup, after schema migration and before the HTTP listener opens, Kestrix reconciles interrupted runs: every run with `status='running'` that has at least one task result in `running` or `pending` is marked `failed` with error `interrupted by server restart`; each such task result is marked `failed` (was `running`) or `skipped` (was `pending`) with the same error; `finished_at` is set.
- A run whose only non-terminal task results are `manual` is left untouched: it is legitimately waiting for approval and `ApproveManualTask` still works after a restart.
- The reconciliation logs one line with the number of runs affected, and each affected run's output gets a trailing line naming the interruption so the UI shows why it ended.
- Reconciliation is idempotent and safe to run on every start.

## Non-goals

- Resuming or re-executing interrupted runs (tasks are not idempotent; the operator re-runs explicitly).
- Multi-instance coordination (a second `serve` process would misclassify the first one's live runs; Kestrix is single-instance today and the guide says so).
- Killing task containers/processes left behind by the previous process (docker mode containers exit when their parent `docker run` dies; local-mode strays are documented).

## Capabilities

### New Capabilities

- `run-lifecycle-recovery`: what happens to in-flight runs across a server restart.

### Modified Capabilities

(none — `task-execution-isolation` "Cancellation terminates task processes" is unaffected; this covers the case where there is no process to terminate.)

## Impact

- `internal/domain/ports.go` `RunStore` gains `FailInterruptedRuns(reason string) (int, error)`; `internal/store/pgstore_run.go` implements it in one transaction reusing the task-result status derivation in `ApplyTaskResult`.
- `cmd/kestrix/main.go` `cmdServe` calls it after `Migrate`.
- Fakes in `internal/mocks` and service tests.
- `docs/architecture.md` (single-instance note), `CHANGELOG.md`.
