# Proposal: remote-run-completion-telemetry

## Why

Currently, Prometheus runtime metrics (`kestrix_runs_total` and `kestrix_run_duration_seconds`) are recorded exclusively in in-process execution paths via `recordRunFinished` in `internal/service/service.go`. When a run executes in `remote` runner mode, job completion and terminal run finalization in `remote_dispatch.go`, `pgstore_remote_job.go`, and `handler_runner_jobs.go` never invoke `recordRunFinished`. Consequently, production dashboards and scrape targets report zero completed runs and missing duration histograms for all remote workloads.

## What Changes

- Invoke `recordRunFinished(projectName, status, duration)` whenever a remote run transitions to a terminal state (`passed`, `failed`, `canceled`).
- Add terminal completion metric hooks in `resumeRemoteRun` and `CompleteRemoteJob` when the final job of a run finishes.
- Hook into remote run cancellation and timeout handlers to ensure terminal counts increment exactly once per run.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `observability`: ensures remote runs record completion status and duration metrics identical to in-process runs.

## Non-goals

- Adding per-remote-job duration histograms (only run-level metrics are tracked).
- Modifying metric names or label schemas.

## Impact

- Updates `internal/service/remote_dispatch.go` and `internal/web/handler_runner_jobs.go`.
- Remote runs become visible in Prometheus metric scrapes under `GET /metrics`.
