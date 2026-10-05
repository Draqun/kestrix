## Why

A remote run that cannot start a task hangs `running` for good: no result, no
job, and the startup sweep leaves it alone because nothing is `pending` or
`running`. Three ways to get there, each reproduced on `main` (`0febb25`) on
Postgres through `RunService.Start`:

- `deploy` (trigger `repo.app.changed`) depends on `lint` (trigger
  `repo.docs.changed`) and only `app` changed. `lint` is not part of the run,
  so the walker waits for a result that never comes. In-process, `deploy` runs.
- No task is triggered. In-process the run is `passed`.
- Starting fails after the run was created, here a source archive that cannot
  be built. `Start` returns the error and the run stays `running`.

## What Changes

- A dependency outside the run (its trigger is not satisfied) no longer holds
  its dependents back, as in an in-process run.
- A run that has nothing to dispatch finishes `passed` at once.
- A start that fails after the run exists finishes it `failed`, naming the
  error. A task whose job cannot be created is recorded `failed`, as the
  walker does in `remote-run-waits-for-dependents`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `remote-runner-execution`: a remote run does not hang at start.

## Non-goals

- Manual gates and `include_manual`: `remote-manual-gates`. Until it lands, a
  run whose only tasks are manual finishes `passed` like any run that
  dispatches nothing.
- The walker's own dispatch failures: `remote-run-waits-for-dependents`.
- Repairing runs that already hang. They stay `running`; cancel them.
- Run metrics for remote runs. None are recorded today, and this change adds
  none.
- Any in-process behaviour.

## Impact

- No schema or port change.
- Code: `internal/service/remote_dispatch.go` (`eligibleForDispatch`,
  `startRemote`), `docs/remote-runners.md`, `CHANGELOG.md`.
