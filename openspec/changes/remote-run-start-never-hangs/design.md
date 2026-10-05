## Context

- `startRemote` (`internal/service/remote_dispatch.go`) resolves the pipeline to
  `nodes`, the triggered tasks only, creates the run, builds the source archive
  and persists the run's snapshot when `nodes` is not empty, and creates a job for
  each task `eligibleForDispatch` returns. `resumeRemoteRun` calls the same
  function after each completion.
- `eligibleForDispatch` leaves a task alone until every dependency has a result.
  Only a task in `nodes` ever gets a result, so a dependency outside `nodes`
  never resolves.
- In-process, `taskLoop` blocks a task only when a dependency failed in this
  run. A dependency that was not triggered is not in the run and holds nothing.
- A run is finished from a completion, or by `finishRun` in-process. A remote run
  with no result and no job has neither, and the startup sweep skips it: it fails
  only runs with a `pending` or `running` result.

```
Start ─▶ Resolve ─▶ CreateRun ─▶ nothing to dispatch ───────────▶ run passed          (D2)
                                   │
                                   ▼
                        archive + snapshot ── fails ────────────▶ run failed          (D3)
                                   │
                                   ▼
                        eligibleForDispatch ── dependency outside the run: ignored    (D1)
                                   │
                                   ▼
                        per task: pending + job ── job fails ───▶ task failed         (D3)
```

## Decisions

### D1. A dependency outside the run is ignored

`eligibleForDispatch` builds the set of names in `nodes` and skips a
dependency that is not in it. This is what `taskLoop` does, by accident of
tracking only failures. A dependency in the run behaves as before: unresolved
until it has a result, blocking when it failed, was canceled or was skipped.
Its signature does not change.

Rejected: dispatching the untriggered dependency. A trigger would stop meaning
"this task runs when these repos change", and in-process does not do it.

Rejected: recording the dependent `skipped`. The pipeline's meaning would
change silently, and in-process runs the task.

An unknown dependency name still fails `Resolve`, so "outside the run" always
means a declared task whose trigger is not satisfied.

### D2. A pass that dispatches nothing finishes the run

When the first `eligibleForDispatch` pass returns no task, nothing will ever
drive the run, so `startRemote` finishes it from its results. They are empty,
and an empty run is `passed`, as `runStatus` says in-process. With D1 this is
the empty `nodes` case plus a pipeline whose roots are all manual, which
`remote-manual-gates` takes over.

### D3. A start that fails finishes the run or fails the task

After `CreateRun`, a failure to build the archive or to persist the snapshot
happens before any job exists: `startRemote` finishes the run `failed` with the
error and returns the error as before. A failure to create one task's job
records that task `failed` with the error, the way the walker does in
`remote-run-waits-for-dependents` D5; jobs already created run on, and the run
settles `failed` when they complete, or at once when there are none.

The error text is stored on the run, where everyone who can read the run sees
it, so it goes through the same secret redaction as an error a runner reports.

Rejected: building the archive before `CreateRun`, so no run exists after
that failure. In-process, a run that cannot start is a `failed` run in the
history; a remote one should read the same.

## Security Considerations

- The stored error can name a path or a host. It is redacted for secret values
  and no more than the HTTP error the caller already receives.
- No new input, endpoint or permission.

## Risks / Trade-offs

- D1 changes what runs for a pipeline that depends on an untriggered task. Such
  a run hung, so nothing that worked changes.
- A failed start now leaves a `failed` run in the history where it left a
  `running` one. That is the point; `kestrix_runs_total` is unaffected, because
  remote runs record no outcome metric today.
- `startRemote` and `eligibleForDispatch` are also edited by
  `remote-run-waits-for-dependents` and `remote-manual-gates`. There is no
  behavioural dependency between the three; whichever lands later rebases.

## Migration Plan

None. No schema change. A run that already hangs is not repaired; cancelling it
works as for any running run.
