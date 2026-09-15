## Context

See proposal.md - Why. Run status is derived from the latest task result per task name in `ApplyTaskResult` (`internal/store/pgstore_run.go:185-215`): any `manual`/`pending`/`running` task keeps the run `running`. Task results are an append-only JSONB list, deduplicated by name keeping the last entry. There is exactly one `serve` process per database today.

```
cmdServe: web.InitServerDeps (JWT key, then DB connect + schema migration)
             ──▶ FailInterruptedRuns("interrupted by server restart")
             ──▶ web.NewServerEWithDeps (wires RunService etc.)
             ──▶ ListenAndServe
                          │ BEGIN
                          │ SELECT id, task_results, output FROM runs WHERE status='running' FOR UPDATE
                          │ for each: latest-per-task; if any running/pending → rewrite those, append output line,
                          │           UPDATE status='failed', error, finished_at=now()
                          │           else (only manual) → skip
                          │ COMMIT ; return count
```

Note (implementation): schema migration and `RunStore` construction were already
fully encapsulated inside `web.NewServerE` (via unexported `initStores`/`NewPG`) -
there was no standalone `Migrate` step or store handle in `cmd/kestrix`. `NewServerE`
was split into `web.InitServerDeps` (JWT-key validation + store construction, same
order/behavior as before) and `web.NewServerEWithDeps` (the rest), so `cmdServe` can
hold the `RunStore` before the server exists without a second database connection.
`NewServerE` itself is unchanged for its other callers.

## Goals / Non-Goals

**Goals:**
- Correct status in one transaction before any request can observe the old state.
- Reuse the existing status vocabulary and derivation; no new status.

**Non-Goals:**
- Resume, retry, or reaping of stray processes.
- Distinguishing "restart" from "crash"; the message is the same.

## Decisions

- **D1: Do it in the store, in SQL plus Go over the JSONB, not in `RunService`.** The service has no notion of "before my own start"; the store already owns the per-task status derivation. Alternative: a heartbeat column updated by running goroutines and a TTL sweep - more moving parts, and unnecessary while single-instance.
- **D2: Run before the listener, not asynchronously.** Guarantees no request sees a ghost. Cost is one query over `status='running'` rows (indexed by `idx_runs_status`), negligible.
- **D3: Leave manual-only runs alone.** `ApproveManualTask` works without an `active` entry (it goes through `RunOne`), so those runs are genuinely resumable. Failing them would destroy a valid pending approval.
- **D4: `pending` → `skipped`, `running` → `failed`.** Mirrors what `runStatus` in the service records when a run errors mid-way (remaining tasks `skipped`), so the UI renders it with existing styles.
- **D5: Append one output line rather than only setting `error`.** The run detail page shows output; the reason should be visible where the user looks.

## Security considerations

None new: the reconciliation writes only status fields and a fixed message; it never reads or logs task output. A stray local-mode process from the previous instance keeps running on the host; documented as a reason to use `docker` mode in production.

## Risks / Trade-offs

- [Two `serve` processes sharing a database would each fail the other's live runs] → Documented single-instance constraint in `docs/architecture.md`; a future multi-instance change adds ownership.
- [A very large `running` backlog on first upgrade] → Bounded by the same unbounded table today; one transaction, indexed scan. Acceptable.

## Migration Plan

Additive, no schema change. First start after upgrade fails whatever ghosts already exist, which is the desired effect. Rollback: previous binary; already-failed rows stay failed.
