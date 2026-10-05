## Why

`remote` is now the main execution mode. Using it by hand surfaced these
defects; the second, found behind the misleading label, runs tasks nobody
asked for.

- **A remote task shows "(No output)".** Runner trace goes only to the run's
  aggregate output (`AppendJobTrace`, `internal/store/pgstore_remote_job.go`).
  The task's result keeps only the completion's leftover chunk, which is
  usually empty. The run detail view reads the task's result.
- **A single-task re-run in `remote` mode can dispatch other tasks.** The
  re-run stores the task name in `runs.changed` and the full project as its
  pipeline. When the job completes, `resumeRemoteRun` resolves the whole DAG
  against that set. Every task whose trigger holds for it becomes eligible
  and is dispatched. Negated triggers hold for any set, e.g.
  `!repo.docs.changed`. With `lint` (`!repo.docs.changed`), `build`, and
  `deploy` after `build`, re-running `build` dispatches `lint` and `deploy`.
  They go out with no source archive and no variables.
- **A single-task re-run is labelled "Changed: <task>"** in every mode,
  because of the same `changed` field. A task name is not a changed
  repository.
- **A revoked runner token gets "missing or malformed Authorization header"**
  (`requireRunnerAuth`). A store error also answers 401. The `kestrix runner`
  process then retries every 5 s, forever.

## What Changes

- A remote task's result carries the output of the claim that completed the
  job: every trace append accepted from that claim plus the completion's
  remainder. It is redacted and bounded per task by `max_output_bytes`.
- A claim starts its trace from scratch. A job that silent-runner recovery
  returned to the queue no longer loses its replacement runner's early output
  from the run's output (found while building the item above): the
  superseded claim's trace position is reset, and the task's output starts
  over.
- A remote run with no remote snapshot is never resumed. Single-task re-runs
  are exactly those runs, so completing one dispatches nothing else. Approving
  a manual task inside a whole run still resumes that run's dependents.
- A single-task re-run records its task in a new `rerun_task` field, leaves
  `changed` empty, and is labelled "Re-run: <task>" in the run list and run
  detail.
- A well-formed but unknown or revoked runner token answers 401 with
  `invalid or revoked runner token`. A store failure answers 500. The
  `kestrix runner` process exits non-zero on 401 from a poll, printing the
  server's message. It keeps retrying other errors.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `remote-runner-execution`: task output from the completing claim; a claim
  starts its trace from scratch; single-task re-runs never resume; rejected
  runner credentials.
- `ui-single-task-rerun`: a re-run is recorded and labelled as one.

## Non-goals

- Live per-task output while a remote task runs. In-process tasks also get
  their output only on completion; the aggregate run output stays live.
- Relabelling single-task re-runs recorded before this change.
- Telling an unknown token apart from a revoked one.
- Re-running a task together with its dependents.

## Impact

- Schema: `remote_jobs.output TEXT NOT NULL DEFAULT ''` and
  `runs.rerun_task TEXT NOT NULL DEFAULT ''`, both idempotent
  `ADD COLUMN IF NOT EXISTS`. The previous image ignores them, so rollback
  stays safe.
- API: runs gain `rerun_task`. A single-task re-run's `changed` becomes `[]`.
  The runner 401 body text changes.
- Code: `internal/store`, `internal/service` (`resumeRemoteRun`),
  `internal/web`, `cmd/kestrix/runner.go`, UI run views,
  `docs/remote-runners.md`.
