## Context

- `CompleteRemoteJob` (`internal/service/remote_dispatch.go`) calls the
  store's `CompleteJob`, then `resumeRemoteRun`, which dispatches what the
  completion unblocked.
- `CompleteJob` (`internal/store/pgstore_remote_job.go`) is one transaction:
  it appends the task result, marks the job, derives the run's status with
  `computeRunStatus` and, when that is terminal, sets `finished_at` and
  prunes old runs.
- `computeRunStatus` sees only recorded results. `startRemote` records
  `pending` for the tasks it dispatches; a task waiting for its dependencies
  has no result until the walker dispatches or skips it.
- `ClaimNextJob` joins `runs` and skips jobs of finished runs.
- `ReclaimStaleJobs` fails a stale job through `CompleteJob`.
- `ApplyTaskResult` recomputes the status and clears `finished_at` when it
  is not terminal, so it can move a finished run back to `running`; manual
  approval relies on that.

```
runner ── complete ──▶ CompleteRemoteJob ─┬─ CompleteJob   result + job   (+ run status: today)
                                          ├─ resumeRemoteRun   due tasks ─▶ pending + jobs
                                          └─ finish the run (new)   status from results, if nothing is in flight
```

## Decisions

### D1. The service finishes the run, after the walker

The port's `CompleteJob` has one protocol: it records the result, the output
and the job, and leaves the run's `status`, `error` and `finished_at` alone.
`CompleteRemoteJob` runs the walker and then finishes the run (D2).

The store keeps a second, private path for silent-runner recovery:
`ReclaimStaleJobs` fails a stale job and ends its run in the same
transaction - when the run's results leave nothing in flight, the same rule
`FinishRunIfDone` follows - which is today's behaviour - a failed stale job
ends the pipeline's useful work. That path does not run the walker, so the
tasks the stale job blocked are neither dispatched nor recorded `skipped`; it
is outside this change's scope, and the requirement in the spec delta says
so.

A run is therefore `running` from the moment a task completes until the
walker has dispatched or skipped what it unblocked. No client reads a
terminal status in between.

Rejected: let the walker reopen the run with `ApplyTaskResult(pending)`. The
run would read `passed` for as long as the walker takes (it loads the whole
source archive), and reopening can revive a canceled run.

Rejected: record every triggered task up front. A task with no job needs a
status of its own, and the walker's test for "already decided" is "has a
result".

Rejected: decide in the store from `pipeline_def`. It needs trigger
evaluation (`dag`), and the store imports only `domain`.

### D2. One method finishes a run from its results

`RunStore` gains `FinishRunIfDone(runID)`. Under a row lock it returns at
once for a finished run; otherwise it derives the status with
`computeRunStatus` exactly as `CompleteJob` did inline and, when terminal,
writes status, error and `finished_at` and prunes history. It never moves a
finished run, so a cancel that wins stays canceled.

### D3. The run's lock covers the whole completion

`CompleteRemoteJob` takes the per-run lock before `CompleteJob` and holds it
through the walker and `FinishRunIfDone`. Two jobs of one run completing
together are then handled one after the other. Without that, completion A
could finish the run on a view that does not yet contain completion B's
result, and B's dependents would be dispatched into a finished run. The lock
is in-process, which suffices while there is one control-plane process (the
assumption `runLocker` already makes).

### D4. The walker leaves a finished run alone

`resumeRemoteRun` already loads the run; it returns when the run is
terminal. A cancel that lands first is not undone by a late dispatch. A
cancel landing between the check and the dispatch is not prevented (the
dispatched jobs are never claimed, because the run is terminal).

### D5. A dispatch failure fails the task

If creating the job of a due task fails, the walker records that task
`failed` with an error naming the dispatch failure, and the run finishes
`failed`. Before, the task kept a `pending` result with no job and the run
had already finished. An error that stops the walker before it knows what is
due (loading the run or the snapshot) does not finish the run: it stays
`running`, visible, cancelable, and failed by the startup sweep if the
process restarts. Finishing it `passed` on such an error would hide the
undispatched tasks.

The finishing write is retried (three attempts): the completion is already
recorded, so a single failed write would leave the run `running` with every
task terminal and nothing that would ever finish it. A write that keeps
failing leaves the run `running` until the startup sweep marks it `failed`
(`interrupted by server restart`) - the same visible failure as a walker
error that stops before the run's status can be decided.

## Security Considerations

- No new data, endpoint or permission.
- A run that stays `running` after a walker error is a visible failure. The
  alternative, a `passed` run with undispatched tasks, is a silent one.

## Risks / Trade-offs

- If the process dies between recording a result and finishing the run, the
  run stays `running` with nothing in flight and the startup sweep marks it
  `failed` (`interrupted by server restart`). Before, the same crash left a
  `passed` run whose dependents never ran.
- The lock is held across one database transaction. It serializes
  completions of a single run only.
- `CompleteJob`'s signature does not change. The port keeps one protocol; the
  store's private reclaim path keeps the old one.

## Migration Plan

None. No schema change; the previous image simply finishes runs the old way.
