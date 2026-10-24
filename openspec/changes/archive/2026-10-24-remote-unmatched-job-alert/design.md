## Context

- Tag matching lives in SQL: `ClaimNextJob` requires
  `rj.required_tags <@ (SELECT tags FROM runners WHERE id = $2)`
  (`internal/store/pgstore_remote_job.go`).
- Runners are listed with `revoked_at IS NULL`. Liveness is computed in Go
  (`domain.RunnerStatus`).
- One background loop exists, `StartRemoteJobReclaimLoop`
  (`internal/web/server.go`, every 15 s). It deletes terminal secrets and
  reclaims silent jobs.
- The metrics registry takes the live `running`/`queued` gauges from outside
  at scrape time (`Registry.Snapshot(running, queued, …)`).
- The UI shows `TaskResult.Error` only as a red alert.

```
remote_jobs (pending) ──┐
                        ├─ UnmatchedPendingJobs(runID?)  ─┬─▶ GET run → task.waiting_reason
runners (not revoked) ──┘   one SQL query                 ├─▶ /metrics → kestrix_remote_jobs_unmatched
                                                           └─▶ reclaim tick → slog.Warn once per job
```

## Decisions

### D1. One query, evaluated on read

One store method lists the pending jobs no non-revoked runner can take. It
takes an optional run ID and returns the job ID, run ID, task and required
tags:

```sql
SELECT rj.id, rj.run_id, rj.task_name, rj.required_tags
FROM remote_jobs rj
WHERE rj.status = 'pending'
  AND NOT EXISTS (SELECT 1 FROM runners r
                  WHERE r.revoked_at IS NULL AND rj.required_tags <@ r.tags)
```

It is the same containment the claim uses, so "unmatched" and "claimable"
cannot disagree. An empty tag set is contained in any runner's tags, so an
untagged job is unmatched only when no runner exists.

Rejected: storing a flag at dispatch. It would go stale as soon as a runner
is registered or revoked, and clearing it needs a writer on every runner
change.

### D2. `waiting_reason` on the task, not `error`

Run responses add `waiting_reason` to the latest result of an unmatched
pending task. The text is `no registered runner has tags: <sorted tags>`,
or `no runner is registered` when the table is empty. The UI renders it as
a warning (amber) under the pending chip. `error` keeps meaning "this task
failed", so the red alert is not reused for a state that can still resolve.

### D3. Gauge at scrape time

`kestrix_remote_jobs_unmatched` (gauge, no labels) is computed like
`running`/`queued`: the metrics handler calls the query and passes the count
into the snapshot. It has no labels, so cardinality is not a concern.

### D4. One log line per job

The reclaim tick calls the query without a run filter and logs
`remote job unmatched by any registered runner` with `run_id`, `task` and
`required_tags` for every job ID not yet logged. An in-memory set of logged
IDs is pruned to the current result, so a restart may log each job once
again. That is acceptable.

## Security Considerations

- `waiting_reason` exposes tag names to anyone who can read the run. Tags are
  already visible in the pipeline definition shown to the same viewers.
  Runner names are not exposed.
- The query is read-only and bounded by pending jobs.

## Risks / Trade-offs

- A run page polled every 1.5 s now runs one more small query per poll, only
  while the run has pending remote jobs.
- A runner registered with the right tags but never started still counts as
  a match. That case shows as `never seen` in Settings → Runners, which is
  where an operator looks next.

## Migration Plan

No schema change. Rollback: the previous image omits the field and the
gauge.
