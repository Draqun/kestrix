## Context

A remote-mode run dispatches each task as a `remote_jobs` row. A runner
claims the row, streams output with `PATCH /api/runner-jobs/{id}/trace`, and
reports with `PUT .../complete`. `CompleteRemoteJob`
(`internal/service/remote_dispatch.go`) then always calls `resumeRemoteRun`.
That walker re-resolves the run's DAG from `run.Changed` and dispatches
every newly eligible task, using the run's remote snapshot (source archive,
per-task variables, secret values).

A whole run gets that snapshot in `startRemote`. A single-task re-run
(`POST /api/projects/{name}/tasks/{task}/run` → `StartTask` →
`dispatchSingleTask`) does not. It dispatches one job from its own
workspace. The run row still gets `changed = [task]` and the full project as
`pipeline_def` (`internal/web/handler_run.go`).

```
runner ── trace chunk ──▶ AppendJobTrace ──▶ runs.output        (aggregate, live)
                                        └──▶ remote_jobs.output (new, per job)
runner ── complete ─────▶ CompleteJob ────▶ task_results[+] { output: job.output + remainder }
                          CompleteRemoteJob ─▶ resumeRemoteRun
                                               └─ no remote snapshot → return (new)
```

## Decisions

### D1. Accumulate per job, write once on completion

`AppendJobTrace` already locks the job row and the run row. It also appends
the same redacted chunk to `remote_jobs.output`, bounded by
`boundAppendChunk` against that column. `CompleteJob` builds the task
result's `output` from that column plus the completion's remainder, bounded
the same way.

Rejected: rewriting the matching entry inside `runs.task_results` on every
chunk. It is an append-only JSON array with several entries per task. That
would be a JSON read-modify-write per chunk for output the UI only shows on
completion anyway.

### D2. Nothing to resume without a snapshot

`resumeRemoteRun` returns before resolving the DAG when
`GetRemoteRunSnapshot` returns no snapshot. Every dispatch it can make needs
that snapshot: without it the payload has no source archive and no
variables. The runs that lack one are exactly single-task re-runs. This
covers rows written before the upgrade too, and needs no new flag.
Approval of a manual task inside a whole run still resumes, because that run
has a snapshot.

Rejected: gating on `rerun_task` (D3). It misses in-flight re-runs created
before the upgrade, and it ties a correctness guard to a display field.

### D3. `rerun_task` is the label, not the trigger set

`CreateRun` gains the re-run task name. The two call sites for task runs
(the task-run handler and its secret-error path) pass it with an empty
`changed`. The UI shows "Re-run: <task>" when `rerun_task` is set; otherwise
it keeps the existing `changed` rendering. Server-side, only
`resumeRemoteRun` reads `run.Changed`, so emptying it is safe once D2 holds.

### D4. Runner credentials: say what failed, and stop on it

`requireRunnerAuth` keeps `missing or malformed Authorization header` for a
missing or non-`Bearer` header. A well-formed token the store rejects answers
`invalid or revoked runner token`. An `AuthenticateRunner` error answers
`500`. This split is what makes the runner change safe: `runPollLoop`
returns an error on `401`, and `cmdRunner` exits non-zero with the body.
A database blip must not stop every runner.

## Security Considerations

- Output: the new column stores the chunk after server-side redaction, the
  same bytes already appended to `runs.output`. No new plaintext at rest.
  It is not returned by any runner-facing endpoint.
- The 401 message is the same for unknown and revoked tokens, so it does not
  reveal whether a token ever existed.
- D2 removes a path where a re-run dispatched tasks (possibly `deploy`)
  that nobody requested.

## Risks / Trade-offs

- A remote task's output is stored twice: in the run's aggregate and in the
  job row until the task result is written. The per-task bound caps it.
  Retention already prunes whole runs.
- A runner revoked mid-job exits on its next poll, after its in-flight
  completion is rejected. That is intended.

## Migration Plan

Two idempotent `ALTER TABLE ... ADD COLUMN IF NOT EXISTS ... NOT NULL DEFAULT
''` in `internal/store/pgstore.go`. Existing rows keep behaving as today
except for D2, which needs no data. Rollback: the previous image ignores
both columns.
