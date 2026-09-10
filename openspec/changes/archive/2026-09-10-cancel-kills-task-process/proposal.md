## Why

Cancelling a run only kills the underlying process when the run was started
through `RunService.Start` (`internal/service/service.go:337`), which is the
only path registered in `s.active`/`s.done` and the only path whose executors
call `setCancelViaProcessGroup` (`internal/runner/runner.go:50,270,303`). A
single-task re-run (`RunService.RunTask`, `service.go:578`) and a manual-task
approval (`RunService.ApproveManualTask`, `service.go:692`) both launch a
process through `RunOne` (`internal/runner/runner.go:568-653`) without
registering anywhere `Cancel` looks. `Cancel` (`service.go:379`) then finds no
active entry and falls back to `runStore.CancelRun`, which only rewrites the
run's status in Postgres — the `bash -c` child `RunOne` started keeps running
to completion. The UI reports the run canceled while the
task, and anything it does (a deploy script, `terraform apply`), keeps going.
This is a regression against the intent of the already-merged
`runner-execution-isolation` change, whose cancellation guarantee never
reached the two call paths added by later changes.

## What Changes

- `RunTask` and `ApproveManualTask` register their run in the same
  `active`/`done` bookkeeping `Start` uses, so `Cancel` treats all three
  starting paths identically instead of only recognizing DAG runs.
- `RunOne` gains the same process-group registration
  (`setCancelViaProcessGroup`) the DAG local/GitLab-runner executors already
  use, so a cancel delivered while a single-task or manual-approval run is
  executing actually terminates the process (SIGTERM to the group, SIGKILL
  after the grace period).

## Non-goals

- Making `RunOne` honor the configured runner mode. Today `PipelineExecutor.
  RunTask` (`internal/runner/pipeline_executor.go:100-104`) always calls
  `RunOne`, and `RunOne` always runs the task locally via `bash -c`
  regardless of whether the server is configured for `docker` mode — a
  single-task re-run or manual approval gets none of the Docker hardening a
  DAG run gets in that mode. That is a real gap but a separate, larger one
  (isolation, not cancellation); this change only makes `RunOne`'s existing
  local execution killable on cancel, it does not add a Docker path to it.
- Changing what `KESTRIX_RUNNER_CANCEL_GRACE` defaults to, or any other
  cancellation timing already fixed by `runner-execution-isolation`.
- Cancelling a run that has already finished, or double-cancel semantics —
  both are already handled by the existing `Cancel` code path this change
  reuses.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `task-execution-isolation`: the "Cancellation terminates task processes"
  requirement currently only covers runs started via the DAG path
  (`RunService.Start`); it must cover `RunTask` and `ApproveManualTask` too.

## Impact

- `internal/runner/runner.go`: `RunOne` gains process-group registration.
- `internal/service/service.go`: `RunTask` and `ApproveManualTask` register
  and deregister in `active`/`done`; `Cancel` needs no change, it already
  walks those maps.
- No API or schema change; `handleCancelRun`, `handleRunTask`, and
  `handleApproveManualTask` (`internal/web/handler_run.go`) call the same
  service methods they call today.
- `internal/runner` and `internal/service` unit-test suites (both held at
  100% statement coverage) need new cases for the two additional start paths.
