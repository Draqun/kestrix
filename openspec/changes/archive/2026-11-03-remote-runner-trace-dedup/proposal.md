# Proposal: remote-runner-trace-dedup

## Why

In `feat/ui-realtime-sse`, real-time Server-Sent Events (SSE) streaming was implemented for pipeline runs and logs. During review (finding R4), architectural and correctness concerns were identified in the remote runner trace streaming path (`internal/web/handler_runner_jobs.go:214`):
1. **Web Layer Publishing & DB Overhead:** Remote trace chunks are published directly by the HTTP handler rather than the service layer, requiring an extra `GetRemoteJob` database query for every chunk.
2. **Duplicate Re-broadcasts on Runner Retry:** `domain.RemoteJobStore.AppendJobTrace` returns `ok=true` for safe idempotency retries where `offset < trace_offset`. The handler broadcasts the chunk unconditionally, causing retried PATCH requests to broadcast duplicate log text and advance offsets past stored output.
3. **Offset Semantics Inconsistency:** `TracePayload.Offset` carries per-job trace offsets for remote runs but per-run aggregate offsets for local runs, preventing reliable client-side deduplication and ordered reconstruction.
4. **Missing Task-Running Event on Claim:** `ClaimNextJob` transitions a task from pending to running without emitting a task state transition event to SSE listeners, causing remote task nodes in the UI DAG to jump directly from pending to terminal status.

Addressing these issues requires changing the `domain.RemoteJobStore.AppendJobTrace` interface contract and database implementations across `PGStore` and `MockRemoteJobStore`, which affects concurrent-append invariants (D9). This work was deferred to a dedicated change to maintain isolation and test stability.

## What Changes

- Update `domain.RemoteJobStore.AppendJobTrace` to return whether new bytes were actually appended versus acknowledged as an idempotent retry.
- Move remote trace publishing out of `handleRunnerJobTrace` in `internal/web/handler_runner_jobs.go` into `RunService` (e.g. `AppendRemoteJobTrace`).
- Stream only newly accepted bytes to the event hub, bounded by the run's configured output limit.
- Emit a `running` task event when a runner claims a job via `ClaimNextJob`.
- Standardize trace offset semantics or implement client-side offset filtering to drop duplicate chunks.

## Capabilities

### Modified Capabilities

- `remote-runner-execution`: Ensures remote runner trace streaming and job claims publish accurate lifecycle events and avoid duplicate log emissions on retries.

## Non-goals

- Altering the runner client's PATCH request protocol or retry schedule.
- Changing in-process/local runner trace handling (already covered by `eventHubTraceWriter`).

## Impact

- Interfaces in `internal/domain` (`RemoteJobStore`).
- Implementations in `internal/store` (`PGStore`) and `internal/mocks` (`MockRemoteJobStore`).
- Service methods in `internal/service` (`RunService`).
- Handlers in `internal/web` (`handler_runner_jobs.go`).
