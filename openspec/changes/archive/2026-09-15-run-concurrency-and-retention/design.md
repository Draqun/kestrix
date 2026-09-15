## Context

See proposal.md - Why. `RunService` tracks in-flight runs in `active`/`done` maps guarded by `mu` (`internal/service/service.go:462-472`); `Start`, `RunTask` and `ApproveManualTask` all go through `trackActive`. Run status derivation lives in `ApplyTaskResult` (`pgstore_run.go:185`) and `runStatus` (service). Webhook history pruning is a `DELETE ... WHERE id NOT IN (SELECT ... LIMIT $1)` inside the insert transaction (`pgstore_webhook.go:77-85`). Config keys and precedence live in `internal/config` with a reflection-based completeness test.

```
Start/RunTask/Approve ──▶ createRun(status=queued if no slot) ──▶ slots chan (cap = max_concurrent_runs)
                                                                   │ acquire ──▶ status=running ──▶ executeRun
                                                                   └ release ──▶ next FIFO waiter
finishRun ──▶ FinishRun ──▶ PruneRuns(limit) [terminal only, oldest first]
redactingWriter ──▶ boundedWriter(max_output_bytes) ──▶ FlushRunOutput
```

## Goals / Non-Goals

**Goals:**
- Backpressure with a visible state, not silent rejection.
- Reuse the webhook-history pruning pattern and the config precedence machinery verbatim.

**Non-Goals:**
- Fairness across projects, priorities, or a persisted queue.

## Decisions

- **D1: Buffered channel as the semaphore, FIFO waiters in a slice under `mu`.** Simplest correct primitive; Go's channel gives blocking acquire with context cancellation for free. Alternative `golang.org/x/sync/semaphore`: a new dependency for the same thing.
- **D2: `queued` is a real stored status, not an in-memory flag.** Lists and counters come from the store; an in-memory flag would make the API lie after a restart and be invisible to `GET /api/runs`. `orphan-run-recovery` treats a `queued` row at startup like `running`-with-pending: failed with the interruption message.
- **D3: Prune in `FinishRun`'s transaction, terminal statuses only.** Same shape as webhook pruning; deletes cascade `runs` rows only (no children). Manual-pending runs have status `running` today, so the predicate is `status IN ('passed','failed','canceled')`.
- **D4: Truncate at the tail in the writer, after redaction.** Redaction needs the full stream to match secrets; truncation after it never leaks a partial secret. Head truncation would lose the start of the log, which is where setup errors appear.
- **D5: Default limit `2`.** Matches the default docker CPU limit of 2 per task container; a customer with a bigger host raises it.

## Security considerations

- Queueing removes an amplification path: one push cannot fan out into unbounded container launches.
- Output bound caps database growth from a hostile or runaway script.
- No new data exposure; `queued` carries the same fields as other statuses.

## Risks / Trade-offs

- [A long-running task blocks the queue] → Existing cancel works on running runs; docs recommend task-level timeouts in scripts. A per-run timeout is a natural follow-up.
- [Retention deletes a run a user is viewing] → The UI already handles a `404` on run detail with an error alert; acceptable at 1000 runs default.
- [Truncated output hides the failing line] → Marker names the bound and the setting to raise it.

## Migration Plan

Additive. Defaults apply to existing deployments on upgrade; a deployment that relied on unbounded parallelism sets `KESTRIX_RUNNER_MAX_CONCURRENT_RUNS` higher. Retention deletes old terminal runs on the first finished run after upgrade; CHANGELOG says so and how to raise the limit first. Rollback: previous binary; `queued` rows (if any) become ghosts that `orphan-run-recovery` cleans, or are cancelled manually.

## Open Questions

- Whether the dashboard should show queue depth as a separate tile or fold `queued` into the existing status counter. Either satisfies the spec; decide in the UI task.
