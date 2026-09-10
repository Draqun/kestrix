## Context

See proposal.md — Why.

- `RunService.Start` (`internal/service/service.go:337-366`) is the only
  place that registers a run in `s.active map[int]context.CancelFunc` and
  `s.done map[int]chan struct{}`. `Cancel` (`service.go:379-421`) looks up a
  run in those two maps; when absent it calls `runStore.CancelRun` directly
  and returns, with no attempt to signal a process.
- `RunService.RunTask` (`service.go:578-636`) and `ApproveManualTask`
  (`service.go:692+`) both call `s.taskRunner.RunTask(ctx, ...)`, which
  `PipelineExecutor.RunTask` (`internal/runner/pipeline_executor.go:80-105`)
  routes unconditionally to `runner.RunOne` — there is no mode branch here,
  unlike `PipelineExecutor.Run`, which does dispatch `local`/`docker`/
  `gitlab-runner`. `RunOne` (`internal/runner/runner.go:569-645`) builds
  `cmd := commandContext(ctx, "bash", "-c", task.Execute.Script)` and calls
  `cmd.Run()` with no `SysProcAttr`, no `cmd.Cancel`, no `cmd.WaitDelay`.
- `setCancelViaProcessGroup(cmd *exec.Cmd, grace time.Duration) (cleanup
  func())` (`runner.go:50-63`) already exists and is called by
  `LocalExecutor.RunTask` and `GitLabRunnerExecutor.RunTask` (`runner.go:270,
  303`) before `cmd.Run()`. It sets `cmd.SysProcAttr = &syscall.
  SysProcAttr{Setpgid: true}`, wires `cmd.Cancel` to send SIGTERM to the
  group, and sets `cmd.WaitDelay` so the stdlib sends SIGKILL to the group if
  the process hasn't exited by then. This is the exact seam `RunOne` needs;
  nothing new has to be invented.
- `commandContext = exec.CommandContext` is the one seam `internal/runner`'s
  unit tests stub, which is what keeps the package at 100% unit-only
  coverage (`runner-unit-only-coverage`). `setCancelViaProcessGroup` mutates
  the `*exec.Cmd` before `Run`, so it is exercised the same way the existing
  `LocalExecutor`/`GitLabRunnerExecutor` tests already exercise it — no new
  test infrastructure is needed, only new call sites to assert against.

```
Today:                                   After this change:

Start ──┐                                Start ──┐
        ├─► active/done ─► Cancel               ├─► active/done ─► Cancel
        │      (waits, kills group)             │      (waits, kills group)
RunTask ┘         ✗ not registered       RunTask ┘
ApproveManualTask  ✗ not registered      ApproveManualTask ┘
        │                                        │
        └─► RunOne ─► bash -c (no SysProcAttr)   └─► RunOne ─► bash -c
                                                         (setCancelViaProcessGroup)
```

## Goals / Non-Goals

**Goals:**
- `Cancel` behaves identically no matter which of the three service methods
  started the run's currently-executing task.
- Reuse `setCancelViaProcessGroup` as-is; no new termination mechanism.

**Non-Goals:** see proposal.md — Non-goals (RunOne's Docker-mode dispatch is
explicitly out of scope).

## Decisions

**D1: Register `RunTask`/`ApproveManualTask` in the same `active`/`done`
maps `Start` uses, keyed by `runID`, rather than adding a second tracking
structure.**
`Cancel` already knows how to wait on `done` and call `runStore.CancelRun`
after — the only reason it doesn't work for these two paths today is that
nothing populates the maps for them. Reusing the existing structure means
`Cancel` needs zero changes; only the two `RunService` methods change.
Alternative considered: a separate `activeTasks` map for these two paths,
inspected by a modified `Cancel`. Rejected — it would duplicate `Cancel`'s
wait/timeout logic for no behavioral difference.

**D2: Wrap the incoming `ctx` with `context.WithCancel` inside `RunTask`/
`ApproveManualTask` themselves, register before calling `s.taskRunner.
RunTask`, deregister via `defer` after it returns — mirroring `Start`'s
`go func() { defer close(doneCh); defer cancel(); defer delete(...); ...
}()` structure, minus the goroutine (these two methods are already called
synchronously from their HTTP handlers, so no goroutine wrapper is needed;
the caller's own `ctx` argument is what `cancel()` derives from).**
Alternative considered: pushing registration into `PipelineExecutor.RunTask`
or `RunOne` itself. Rejected — those live in `internal/runner`, which has no
concept of `RunService`'s `active`/`done` maps or `runID`; keeping
registration in `internal/service` keeps the layering `hexagonal-ports`
established.

**D3: Add `setCancelViaProcessGroup(cmd, grace)` to `RunOne` at the same
point `LocalExecutor`/`GitLabRunnerExecutor` add it — immediately before
`cmd.Run()` — using the grace period already read from `config.Current()`
the same way those two executors do.**
No alternative considered; this is a direct application of the existing
pattern to the one remaining call site that lacked it.

## Risks / Trade-offs

- [Registering in `active`/`done` for a synchronous call changes what
  `Cancel` sees while `RunTask`/`ApproveManualTask` are still running: a
  second concurrent `Cancel` call for the same `runID` during that window
  now waits on `doneCh` instead of hitting the store directly.] → This is
  the intended fix, not a side effect: today that second call would report
  "canceled" while the process runs on; after this change it correctly waits
  for termination or times out, exactly like a DAG run.
- [`ApproveManualTask`'s deregistration must run even when `s.taskRunner.
  RunTask` panics or returns early via `opts.WorkspaceCleanup`'s defer
  ordering.] → Use `defer` for map cleanup ahead of the `WorkspaceCleanup`
  defer already present, same ordering `Start`'s goroutine already relies on.

## Security Considerations

This closes a real control gap, not just a UX one: a pipeline is untrusted
input (per `task-execution-isolation`'s own Purpose statement), and today a
manual-approval or single-task script keeps running after an operator
believes they stopped it — including any external mutation the script makes
(a `terraform apply`, a `kubectl` rollout, a registry push). Killing the
process group on cancel is the only way "canceled" is an honest status.
No new attack surface is introduced: `setCancelViaProcessGroup` is existing,
already-reviewed code being called from one more site.

## Migration Plan

No data migration. Deploy as a normal code change; no config flag, no
behavior change for already-passing cancellation scenarios (DAG runs), only
new coverage for the two previously-unregistered paths. Rollback is a
plain revert — no persisted state changes shape.
