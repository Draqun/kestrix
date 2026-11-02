# Design: remote-run-completion-telemetry

## Context

Metrics recording in Kestrix is centralized in `RunService.recordRunFinished`. In-process runs call this helper in `finishRun`. In `remote` execution, runs advance asynchronously as runners complete jobs via `PUT /api/runner-jobs/{id}/complete`. The terminal transition logic resides in `resumeRemoteRun` in `internal/service/remote_dispatch.go`. Currently, when `resumeRemoteRun` determines that all tasks are finished and marks the run `passed` or `failed`, it omits calling `recordRunFinished`.

## Goals / Non-Goals

**Goals:**
- Guarantee every remote run that finishes records its outcome and duration in Prometheus metrics.
- Prevent duplicate metric recording if a remote job completion races with a cancellation request.

**Non-Goals:**
- Creating separate metrics for remote vs local runs (they share the unified `kestrix_runs_total` metric).

## Decisions

### D1: Call `recordRunFinished` inside `finalizeRemoteRun`
- **Choice:** Consolidate remote run finalization into a helper that calculates elapsed duration from `run.created_at` and calls `s.recordRunFinished(run.ProjectName, terminalStatus, duration)`.
- **Alternatives considered:** Calling it in HTTP handler `CompleteRemoteJob`. Rejected because runs with multiple tasks finalize only after the last task, which is evaluated inside the service layer (`resumeRemoteRun`).

## Risks / Trade-offs

- **[Risk]** Clock skew or nil `created_at` causing negative durations.  
  → *Mitigation:* Guard duration calculation with `time.Since` and ensure a non-negative floor.
