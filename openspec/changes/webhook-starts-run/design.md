## Context

`handleWebhook` verifies the delivery and matches registered projects by
repository URL. For each match, `resolveOne` loads the pipeline:
- a primary push loads it at the pushed SHA;
- a secondary push loads it at the configured `repo_ref`.

`resolveOne` computes the changed set, and `resolveAndDispatch` resolves the
DAG, records history and optionally calls GitLab dispatch. `resolveOne`
defers `session.Cleanup()`, so the loaded workspace dies with the request.

A manual run (`handleRunProject`, `internal/web/handler_run.go`) does the
same load, then `service.BuildVarStore` and `RunService.Start` with
`WorkDir`, `InitialSources`, `WorkspaceCleanup`, `Mode` and `Actor`. `Start`
queues or runs in the background and returns at once.

```
POST /webhook ─ verify ─ match ─┬─ resolveOne (per project)
                                │    load pipeline @ SHA / repo_ref
                                │    changed set, DAG resolve, history
                                │
                                ├─ dispatch configured ──▶ GitLab CI      (unchanged)
                                ├─ push to configured ref ─▶ startProjectRun ─▶ RunService.Start
                                │                            (session handed over, not cleaned)
                                └─ otherwise ──▶ recorded only
```

## Decisions

### D1. One run-start path for UI and webhook

Extract `startProjectRun(ctx, cfg, proj, session, changed, actor)` from
`handleRunProject`: `BuildVarStore` (secret errors go through
`failRunOnSecretError` as today), then `RunService.Start`. It returns the run
ID. Both handlers call it. The webhook passes the session it already loaded
and hands over its cleanup instead of deferring it, so the pipeline and
sources the run executes are exactly the ones that resolved it.

### D2. Only the configured ref starts a run

`PushEvent` keeps whether the ref was a branch (`refs/heads/`) or a tag.
Today both are stripped into `Branch`, so a tag `main` would look like the
branch `main`. A run starts when all of these hold:
- the push is a branch push;
- for a primary match, the branch equals the project's `repo_ref`;
- for a secondary match, it equals the matched `ci_repo`'s `ref`. When that
  `ref` is unset, the branch must equal the default branch the payload
  reports.

Rationale: a run gets the project's secrets and deploy tasks. Any branch
running them would let anyone who can push a branch execute with them.
Per-branch policy is a separate decision (non-goal).

### D3. Dispatch replaces the run

When `dispatchTarget` resolves a GitLab project ID and dispatch is enabled,
the project keeps today's behaviour and no Kestrix run starts. Running both
would execute the same tasks twice on two systems.

### D4. Deletion is a recorded no-op

A push whose checkout SHA is all zeros, or whose payload marks it deleted,
is recorded `ignored` with the reason `branch deleted` before any pipeline
load. Today `ParseGitLabPushEvent` falls back from an empty `checkout_sha`
to `after`, so a deletion reaches the pipeline load with the zero SHA on the
dispatch path too.

### D5. Attribution

`webhook:<source name>`, where the source name is the matched project or
`ci_repo` name already recorded in history as `source_name`. It names what
was pushed; the SHA is on the run's sources.

## Security Considerations

- Only a delivery that passed verification can start a run. The rules are
  unchanged: fail closed, constant-time compare, project or global secret.
- D2 keeps secret-bearing runs to the configured ref. A signed replay of an
  old delivery can start a run at an old SHA of that ref. That needs the
  webhook secret, which already allows arbitrary dispatch today; noted, not
  mitigated (non-goal: deduplication).
- The run executes the pipeline file from the pushed revision, as a manual
  run of that revision would. Who may push to the configured ref is the Git
  host's branch protection.

## Risks / Trade-offs

- Behaviour change: pushes now execute. Intended, and there are no published
  installs to surprise.
- The webhook response now waits for `Start` to record the run. That is
  synchronous and short; execution stays in the background, so Git host
  timeouts are not at risk.
- A burst of pushes queues runs behind `KESTRIX_RUNNER_MAX_CONCURRENT_RUNS`
  like manual runs; no per-project coalescing.

## Migration Plan

No schema change. Rollback: the previous image goes back to recording
pushes without running them.
