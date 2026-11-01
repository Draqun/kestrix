# Proposal: task-timeout

## Why

If a task script deadlocks, encounters an infinite loop, or hangs waiting for interactive input, Kestrix currently has no per-task execution timeout. The task runs indefinitely, occupying concurrency slots on runners (`runner.max_concurrent_runs`), starving other builds, and consuming CPU/RAM until an operator manually cancels the run from the UI or restarts the runner process.

## What Changes

- Tasks declared in HCL `ci_task` blocks can specify an optional `timeout` attribute with a duration string (e.g. `timeout = "15m"` or `timeout = "30s"`).
- Default timeout: none (or instance-level fallback if configured).
- When a task execution exceeds its declared timeout:
  - In `local` mode: the task's process group receives SIGTERM, followed by SIGKILL after `KESTRIX_RUNNER_CANCEL_GRACE` (default 10s).
  - In `docker` mode: the task container is stopped via `docker stop -t 10`.
  - In `remote` mode: the runner terminates the child process/container and reports completion with status `failed` and error reason `execution timed out after <duration>`.
- The task result is recorded as `failed` with error reason `execution timed out after <duration>`.
- Dependent downstream tasks transition to `skipped` with `dependency <task> failed`.

## Capabilities

### Modified Capabilities

- `pipeline-definition`: Declare optional `timeout` duration attribute on `ci_task` block.
- `task-execution-isolation`: Enforce execution deadlines across `local`, `docker`, and `remote` execution environments.

## Non-goals

- Sub-second microsecond timer precision (durations use standard Go `time.ParseDuration` format: s, m, h).
- Dynamic per-step timeouts inside shell scripts.
- Modifying timeouts while a task is already executing.

## Impact

- `internal/domain`: `Task` model gains `Timeout string` and parsed duration method.
- `internal/parser`: HCL parser parses and validates `timeout` attribute in `ci_task` blocks.
- `internal/service`: In-process executors wrap execution context with `context.WithTimeout`.
- `cmd/kestrix/runner.go`: Remote runner parses task timeout from job payload and applies execution deadline.
