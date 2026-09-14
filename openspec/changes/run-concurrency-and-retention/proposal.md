## Why

Every accepted webhook or manual trigger starts a run immediately in its own goroutine (`RunService.Start`, `internal/service/service.go:503`; called from `internal/web/handler_run.go:63` and the webhook path). There is no semaphore, queue, or limit, so a burst of pushes, or one delivery fanning out to several projects, launches that many concurrent runs, each with its own workspace and (in docker mode) container. Separately, `runs.output` is an unbounded `TEXT` column and nothing ever deletes runs, while webhook history is already bounded by `KESTRIX_WEBHOOK_HISTORY_LIMIT`. A customer running Kestrix for a few weeks gets a host saturated by parallel jobs and a table that only grows.

## What Changes

- New setting `runner.max_concurrent_runs` / `KESTRIX_RUNNER_MAX_CONCURRENT_RUNS` (default `2`, must be a positive integer). Runs beyond the limit are created with status `queued` and start in FIFO order as slots free up. Cancelling a `queued` run marks it `canceled` without executing anything. Manual task approvals and single-task re-runs take a slot like any run.
- `queued` appears in run lists, the dashboard counters, and the UI with its own style; `GET /api/runs?status=queued` works.
- New setting `runner.run_history_limit` / `KESTRIX_RUN_HISTORY_LIMIT` (default `1000`, positive integer). After a run finishes, runs beyond the limit are deleted oldest-first, never deleting a `running`, `queued`, or manual-pending run. Same mechanics as the webhook history limit.
- New setting `runner.max_output_bytes` / `KESTRIX_RUNNER_MAX_OUTPUT_BYTES` (default `10485760`, 10 MiB). Output beyond the bound is truncated at the tail with a marker line; the run still completes and its status is unaffected.
- All three follow the `runtime-configuration` precedence rule (flag > file > env > default) and appear in the startup source report.

## Non-goals

- Per-project concurrency limits or priorities.
- Persisting the queue across restarts (a `queued` run at restart is handled like an interrupted run by `orphan-run-recovery`: failed with a clear message).
- Distributed workers.
- Streaming output to object storage.

## Capabilities

### New Capabilities

- `run-scheduling`: concurrency limit, the `queued` status and its transitions, run history retention, and the output bound.

### Modified Capabilities

- `runtime-configuration`: "Deterministic configuration precedence" gains the three new keys in its table of multi-form settings.

## Impact

- `internal/config` (three keys, env parsing, precedence test table), `internal/service/service.go` (`RunService` slot acquisition, `queued` handling in `Start`, `RunTask`, `ApproveManualTask`, `Cancel`), `internal/store/pgstore_run.go` (`queued` in status derivation, `PruneRuns`, output truncation on flush), `internal/domain/ports.go`.
- `ui/src/pages/Pipelines.jsx`, `Dashboard.jsx` (status style + counter), `docs/usage.md`, `.env.template`, `docker/docker-compose.prod.yml` pass-through, `CHANGELOG.md`.
