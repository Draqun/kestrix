# Design: stale-job-reclaim-telemetry

## Context

`PGStore.ReclaimStaleJobs` executes background checks on expired remote jobs. When a stale job exceeds `maxReclaims`, it calls internal helper `completeJob(..., finishRun=true)`. If this completes the run, the database transaction updates `runs.status = 'failed'`.

However, the port `domain.RemoteJobStore.ReclaimStaleJobs` currently returns only `(reclaimed, finalizedFailed int, err error)`. `RunService.ReclaimStaleRemoteJobs` has no visibility into which runs were transitioned, preventing Prometheus metric recording and event publishing.

## Goals / Non-Goals

**Goals:**
- Return terminated run IDs from `ReclaimStaleJobs`.
- Finalize telemetry and broadcast terminal events for each terminated run.

**Non-Goals:**
- Changing existing reclaim timeout defaults.

## Decisions

### D1: Extend `ReclaimStaleJobs` signature
- **Choice:** Update `domain.RemoteJobStore.ReclaimStaleJobs` to return `(reclaimed int, finalizedFailed int, finishedRunIDs []int, err error)`.
- **Alternatives considered:** Querying the store afterwards for runs in failed status. Rejected because it risks races and double-counting.

### D2: Lock run and call `finalizeRemoteRun` in service
- **Choice:** In `RunService.ReclaimStaleRemoteJobs`, iterate over `finishedRunIDs`, acquire `s.runLocks.lock(runID)`, and invoke `s.finalizeRemoteRun(runID)`.
