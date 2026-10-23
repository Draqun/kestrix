## Context

- `eligibleForDispatch` (`internal/service/remote_dispatch.go`) skips every
  manual task without recording it: "dispatched only by its own approval".
  `startRemote` records `pending` only for what it dispatches, and
  `computeRunStatus` derives the run's status from recorded results. A gate with
  no result is invisible, and the run finishes without it.
- Approval is `handleApproveManualTask` → `ApproveManualTask` → `RunTask`, which
  in a remote project is `dispatchSingleTask`. It needs the task's `manual`
  result, re-opens the run's recorded sources (`openRecordedRunSources`, which
  fails with `run has no recorded primary source` when there are none), and
  requires every dependency to have `passed`.
- The UI offers Run on a `manual` task only once its dependencies have passed
  (`isManualWaiting`).
- In-process, `taskLoop` records `manual` for a manual task unless
  `include_manual` is set, and `runStatus` keeps the run `running`.

```
Start ─▶ CreateRun ─▶ record the run's sources                                   (D4)
              │
              ▼
        eligibleForDispatch(nodes, latest, includeManual)
              ├─ manual, no include_manual ─▶ gate: result `manual`, before any job   (D1)
              ├─ ready ─────────────────────▶ pending + job
              └─ blocked ───────────────────▶ skipped

walker, after each completion:
              └─ gate whose dependency failed ─▶ skipped                         (D2)

approval ─▶ `manual` result ─▶ job for the gate   (existing; needs D1 and D4)
```

## Decisions

### D1. A gate is recorded when the run starts, before its first job

For every triggered manual task of a run without `include_manual`, `startRemote`
appends a `manual` result before it creates any job. `eligibleForDispatch` takes
the run's `include_manual` and returns the gates to record; it never returns a
gate as eligible, so the walker still never dispatches one. The dependents of a
gate wait: `manual` is already an unresolved state for a dependency.

Before the first job, because a fast runner could otherwise complete `build`
before the gate exists, and `CompleteJob` would see only `passed` and finish the
run.

At the start rather than when the dependencies pass, because the run then stays
open without relying on the completion sequence: `CompleteJob` finishes the run
before the walker records anything, the race `remote-run-waits-for-dependents`
fixes. A gate shown early cannot be approved early: the UI offers Run once the
dependencies have passed, and `ApproveManualTask` checks them again.

Rejected: recording the gate from the walker when its dependencies pass. The
rule is the cleaner one, but until that change is applied the run reads
`passed` between the completion and the walker. Revisit it then.

### D2. A gate behind a failed dependency is skipped

`eligibleForDispatch` re-examines a `manual` result for one case: a dependency
inside the run that failed, was canceled or was skipped. It reports the gate in
`newlySkipped` with `dependency failed`, like any blocked task, and the run can
finish `failed`. Otherwise the run would stay `running` for a gate nobody can
approve, since `ApproveManualTask` requires passed dependencies. In-process this
is what happens (checked: the gate stays `manual`, the run `running`); that is
not copied.

### D3. `include_manual` makes a manual task ordinary

`startRemote` takes `opts.IncludeManual`, the walker `run.IncludeManual` (stored
by `CreateRun`). With it set a manual task is not a gate: it is dispatched when
its dependencies have passed, as in-process.

### D4. A remote run records its sources

Right after `CreateRun`, `startRemote` stores `opts.InitialSources` with
`UpdateRunSources`, as `prepareRunExecution` does in-process through the
`RunSourceStore` the store implements. Approval reads them back. A failure
returns the error, like every other step of the start. The stored declaration
holds the URL, ref, revision and credential reference, never a credential
value.

### D5. The stale requirement is rewritten

`fix-pipeline-manual-status` says a run is `passed` while manual tasks wait.
`computeRunStatus`, `runStatus`, the comment in `finishRun` and
`run-lifecycle-recovery` ("the run still reports `running`") say `running`. The
delta renames that requirement and states what the code does, so the gate
requirement added here does not contradict it.

## Security Considerations

- Approval is an existing action with its existing authorization (operator
  role) and attribution (`approved_by`, `approved_at`). This change makes it
  reachable in a remote project; it grants nothing new.
- Approval resolves the run's secrets again when it dispatches, as in-process
  ("Manual approval resolves fresh").
- The recorded sources hold no credential value.

## Risks / Trade-offs

- A run with a gate stays `running` until someone approves or cancels it, for as
  long as that takes. It is what in-process does, and retention never deletes
  such a run.
- A run in flight at upgrade has no gate result. The walker records a gate it
  finds unrecorded on the run's next completion, so such a run gains it then.
- Nothing pins a remote run's sources while it waits. If the origin no longer
  has the revision, approval fails with the source error.
- `eligibleForDispatch` gains a parameter and a return value; its callers are
  `startRemote` and `resumeRemoteRun`.
- It edits the same functions as `remote-run-waits-for-dependents` and
  `remote-run-start-never-hangs`. There is no behavioural dependency between
  the three, except that the dependents of an approved gate need the first.

## Migration Plan

None. No schema change; `runs.sources` already exists.
