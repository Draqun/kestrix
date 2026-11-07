# Proposal: stale-job-reclaim-telemetry

## Why

When remote runners go silent or become unreachable, `ReclaimStaleRemoteJobs` periodically checks for abandoned jobs via `RemoteJobStore.ReclaimStaleJobs`. If a job exceeds its maximum retry threshold (`reclaim_count >= maxReclaims`), the store fails the job and, when no other jobs remain in flight, completes the run with status `failed` within the store transaction.

Currently, this terminal transition happens solely inside `pgstore_remote_job.go` without returning the finalized run IDs to `RunService`. As a result:
1. `kestrix_runs_total{status="failed"}` counter and `kestrix_run_duration_seconds` histogram are not recorded for runs failed by silent-runner reclaim.
2. `EventHub` is not notified of the run's terminal state.

This was identified during review of `remote-run-completion-telemetry` (finding R1) and deferred to this dedicated change due to the need for domain port contract extension.

## What Changes

- Extend `domain.RemoteJobStore.ReclaimStaleJobs` to return the IDs of runs finalized to terminal state during stale job cleanup.
- Update `RunService.ReclaimStaleRemoteJobs` to acquire run locks and invoke `finalizeRemoteRun` for each reclaimed run.
- Broadcast run state updates to `EventHub`.

## Capabilities

### Modified Capabilities

- `observability`: runs failed due to silent runner timeouts record terminal telemetry metrics (`kestrix_runs_total` and duration).

## Non-goals

- Modifying the retry or timeout algorithm for stale jobs.
- Changing in-process runner reclaim mechanisms.
