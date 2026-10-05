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
                                        └──▶ remote_jobs.output (new, per claim)
runner ── complete ─────▶ CompleteJob ────▶ task_results[+] { output: job.output + remainder }
                          CompleteRemoteJob ─▶ resumeRemoteRun
                                               └─ nothing due, or no remote snapshot → return (new)
```

## Decisions

### D1. Accumulate per job, write once on completion

`AppendJobTrace` already locks the job row and the run row. It also appends
the same redacted chunk to `remote_jobs.output`. The bound is the
aggregate's rule (`boundAppend`, the rule behind `boundAppendChunk`) applied
to that column alone, so a sibling that used up the run's budget does not
shorten this task's output. The check reads only the column's length and
whether it already ends with the marker, in the database: a long task does
not load its output on every chunk. A cut never splits a multi-byte
character, because PostgreSQL rejects the invalid UTF-8 it would leave.
`CompleteJob` builds the task result's `output` from that column plus the
completion's remainder, which goes on its own line when the trace ended
mid-line, bounded the same way.

The column belongs to one claim (D5). When the job ends - completed, fenced
out because its run is already terminal, or cancelled - the column is
emptied: the task result, or the run's aggregate, already has the content.

Rejected: rewriting the matching entry inside `runs.task_results` on every
chunk. It is an append-only JSON array with several entries per task. That
would be a JSON read-modify-write per chunk for output the UI only shows on
completion anyway.

### D2. Nothing to resume without a snapshot

`resumeRemoteRun` works out which tasks are due first (the walk is pure) and
returns when none is: most completions unblock nothing, and the snapshot
holds the run's whole source archive. When something is due it loads the
snapshot, and when `GetRemoteRunSnapshot` returns none it returns before any
write - it never dispatches, skips or records a task. Every dispatch needs
that snapshot: without it the payload has no source archive and no
variables. The runs that lack one are exactly single-task re-runs. This
covers rows written before the upgrade too, and needs no new flag.
Approval of a manual task inside a whole run still resumes, because that run
has a snapshot - also when nothing was dispatched at start because its first
task is manual: `startRemote` stores the snapshot whenever any task is
triggered.

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

### D5. A claim starts its trace from scratch

The runner builds a fresh trace writer per claim, so its offsets start at 0.
`AppendJobTrace` takes an offset below the stored one for a retry and
acknowledges it without storing anything. A job returned to the queue kept
the superseded claim's `trace_offset`, so the replacement runner's early
output vanished from the run's output (reproduced on `main`). `ClaimNextJob`
now resets `trace_offset` and `remote_jobs.output`. The task result comes
from the claim that completes the job; the run's aggregate keeps what every
claim streamed.

Rejected: continuing the superseded claim's output. Two attempts of one task
would run together in one task result.

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
  job row until the job ends. A job returned to the queue keeps the
  superseded claim's output until it is claimed again or cancelled. The
  per-task bound caps it. Retention already prunes whole runs.
- A runner revoked mid-job exits on its next poll, after its in-flight
  completion is rejected. That is intended.

## Migration Plan

Two idempotent `ALTER TABLE ... ADD COLUMN IF NOT EXISTS ... NOT NULL DEFAULT
''` in `internal/store/pgstore.go`. Existing rows keep behaving as today
except for D2 and D5, which need no data. Rollback: the previous image ignores
both columns.
