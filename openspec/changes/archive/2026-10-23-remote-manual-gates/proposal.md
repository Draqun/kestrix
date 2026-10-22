## Why

Manual gates do not work in a `remote` project. Reproduced on `main`
(`0febb25`) on Postgres through `RunService.Start`:

- `build` → `deploy` (manual). The walker never records a manual task, so after
  `build` passes the run is `passed` and `deploy` has no result. The approval
  handler answers `404 task not in run` for a task without one (read from the
  code). In-process, `deploy` is `manual` and the run stays `running` until it
  is approved.
- `include_manual` is ignored: after `build`, no job exists for `deploy` and the
  run is `passed`. In-process, `deploy` runs.
- Recording the gate would not be enough. Approval re-opens the run's recorded
  sources, and a remote run records none (`runs.sources` stays empty although
  `Start` was given its source), so approval would answer `run has no recorded
  primary source`. Read from the code; not run.

## What Changes

- A triggered manual task of a remote run is recorded `manual` when the run
  starts, before its first job. The run stays `running` until the task is
  approved and done; approval dispatches it, as the spec already says.
- A gate whose dependency fails, is canceled or is skipped is recorded
  `skipped`, so the run can finish.
- With `include_manual`, a manual task is an ordinary task.
- A remote run records the sources it started from.
- The `fix-pipeline-manual-status` requirement that a run with waiting manual
  tasks is `passed` is corrected: the code and `run-lifecycle-recovery` say
  `running`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `remote-runner-execution`: manual tasks in a remote run.
- `fix-pipeline-manual-status`: a run with a task awaiting approval is running.

## Non-goals

- Dependents of an approved gate. They run once `remote-run-waits-for-dependents`
  is applied; the approved gate is the last running task there too.
- Pinning a remote run's sources while it waits, as the in-process path does.
  Approval re-opens the recorded revision from the origin.
- A gate behind a dependency outside the run. The UI and `ApproveManualTask`
  need every dependency to have passed, so it cannot be approved, in-process
  either.
- In-process gates. Dependents of a waiting manual task run at once, and a gate
  behind a failed task leaves the run `running` (both checked); unchanged.
- Run metrics for remote runs.

## Impact

- No schema or port change.
- Code: `internal/service/remote_dispatch.go` (`eligibleForDispatch`,
  `startRemote`, `resumeRemoteRun`), `docs/remote-runners.md`, `CHANGELOG.md`,
  and the `fix-pipeline-manual-status` spec.
