## Why

In a whole `remote` run, a task that depends on another stays `pending`
forever when the last running task unblocks it. `deploy` after `build` never
runs, and the run reads `passed`.

- `CompleteJob` records the finished task and, in the same transaction,
  derives the run's status from the recorded results. A task still waiting
  for its dependencies has no result, so after `build` passes nothing is
  `pending` or `running` and the run becomes `passed`.
- Only then does `resumeRemoteRun` record `deploy` as `pending` and create
  its job. `ClaimNextJob` skips jobs of finished runs, so no runner takes it.
- Reproduced on `main` (`42db8c1`) on Postgres: run `passed`, `deploy`
  `pending` in its result and its job, `ClaimNextJob` returns nothing.
- The walker's tests never check the run's status in between, so they
  stayed green.

A script or deploy gate waiting for `passed` acts on a run whose last task has
not run. `remote-task-artifacts` assumes `build` → `deploy` works.

## What Changes

- A remote run finishes only after `resumeRemoteRun` has decided what the
  completed task unblocks. Until then it stays `running`, so no client sees a
  terminal status for a run that still has a task to dispatch.
- Recording the result, resuming the run and finishing it happen under the
  run's lock. `CompleteJob` can record a result without finishing the run;
  the service finishes it afterwards, once the results show nothing in flight.
- The walker acts only on a run that is not already finished.
- A dependent that cannot be dispatched is recorded `failed`, naming the
  error, so the run ends `failed` instead of staying `running` with a task
  that has no job.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `remote-runner-execution`: a remote run finishes after its dependents are
  decided.

## Non-goals

- Manual gates. A manual task has no result until approved, so the run still
  finishes without it and the approval still reopens it.
- A task whose dependency was not triggered. It is neither dispatched nor
  skipped today (checked: `eligibleForDispatch` returns nothing for it), and
  stays so.
- Silent-runner recovery. Failing a stale job still finishes the run in the
  same transaction.
- `ApplyTaskResult` moving a finished run back to `running`.
- Recording dependents as `skipped` after a failure has already finished the
  run.

## Impact

- No schema change.
- Ports: `RemoteJobStore.CompleteJob` gains a flag; `RunStore` gains a method
  that finishes a run from its results.
- Code: `internal/store`, `internal/service` (`CompleteRemoteJob`,
  `resumeRemoteRun`), mocks, `docs/remote-runners.md`, `CHANGELOG.md`.
