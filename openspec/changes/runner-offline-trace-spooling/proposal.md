# Proposal: runner-offline-trace-spooling

## Why

When remote runners execute tasks behind home routers, Wi-Fi networks, or constrained egress environments, transient network hiccups cause trace chunk uploads (`PATCH /api/runner-jobs/{id}/trace`) to fail. Today, the runner logs an error to `stderr` and discards the chunk, causing permanent gaps in task output and triggering server offset mismatch errors (`trace offset is ahead of expected`) on subsequent chunks.

## What Changes

- Remote runner's `traceWriter` gains a local trace spool queue in memory with bounded capacity (`KESTRIX_RUNNER_TRACE_SPOOL_MAX_BYTES`, default 10 MiB).
- When a `PATCH /api/runner-jobs/{id}/trace` request fails due to connection loss, timeout, or 5xx error:
  - The chunk remains queued in the spool buffer.
  - Subsequent stdout and stderr chunks append sequentially to the spool in strict byte-offset order.
  - The runner retries flushing spooled chunks using exponential backoff with jitter.
- The runner flushes all remaining spooled trace chunks before invoking `PUT /api/runner-jobs/{id}/complete`.
- If the spool buffer reaches its memory limit, the runner applies a truncation marker to oldest buffered chunks while maintaining consecutive offset calculations.
- If the server rejects a trace append with HTTP 409 Conflict (indicating the job was reclaimed and epoch advanced), the runner purges its spool and stops the job without overwriting data.

## Capabilities

### Modified Capabilities

- `remote-runner-execution`: Spool unsent trace chunks during network disruptions and flush sequentially with offset integrity.

## Non-goals

- Unlimited disk-backed database for offline logs.
- Extending job execution beyond the server-side reclaim timeout.
- Compressing or altering log chunk payload formats.

## Impact

- `cmd/kestrix/runner.go`: `traceWriter` refactored to manage a thread-safe chunk spool with retry loop and completion flush.
- `internal/config`: Add runner flag and env `KESTRIX_RUNNER_TRACE_SPOOL_MAX_BYTES`.
