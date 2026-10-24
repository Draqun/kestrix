## Why

A remote-mode task declaring `runner_tags` that no registered runner carries
waits in `pending` forever, and nothing says why.

- `ClaimNextJob` offers a job only to a runner whose tags contain its
  `required_tags`.
- None of the dispatch paths (`startRemote`, `resumeRemoteRun`,
  `dispatchSingleTask`) checks whether any runner can ever satisfy them.
- Silent-runner recovery handles `claimed`/`running` jobs only.
- The run detail view shows a grey `PENDING` chip, the same as a job that is
  simply next in line.

The same happens with no runner registered at all. `remote-runner-management`
accepted this as a known risk ("the job stays `pending`, visible as such").
With `remote` becoming the production default (`safe-production-execution`)
and pushes starting runs (`webhook-starts-run`), a typo in a tag turns every
push into a silently stuck run.

## What Changes

- **Unmatched** means: a `pending` job for which no non-revoked registered
  runner has all of its required tags. That includes the case where no
  runner is registered at all. Offline runners still count, because they are
  a transient state that liveness already shows.
- The run API marks such a task with
  `waiting_reason: "no registered runner has tags: gpu, arm64"` (or
  `"no runner is registered"`). The run detail view shows it as a warning
  next to the pending task. The reason is computed on read, so it disappears
  the moment a matching runner is registered.
- `GET /metrics` gains the gauge `kestrix_remote_jobs_unmatched`, the number
  of unmatched pending jobs, for alerting.
- Kestrix logs a warning once per unmatched job, at dispatch or on the
  periodic reclaim tick. The log names the run, task and missing tags.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `remote-runner-execution`: unmatched pending jobs are reported.
- `observability`: the metric set includes the unmatched-jobs gauge.

## Non-goals

- Failing or canceling an unmatched job automatically. Some operators start
  a matching runner on demand; cancel stays manual.
- Rejecting a run at start because no runner matches. The runner may be
  registered a minute later.
- Alerting on matching runners that are merely offline.
- Notifications (mail, chat). The gauge is the hook for an external
  alerting system.

## Impact

- API: task results in run responses gain optional `waiting_reason`.
- Metrics: one new gauge.
- No schema change.
- Code: `internal/store` (unmatched-jobs query), `internal/web` (run
  responses, metrics snapshot, reclaim tick), `internal/metrics`,
  `ui/src/pages/Pipelines.jsx`, `docs/remote-runners.md`.
