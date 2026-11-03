# Design: remote-runner-trace-dedup

## Context

Kestrix runners stream stdout/stderr chunks using `PATCH /api/runner-jobs/{id}/trace` with an offset. The server stores trace data via `RemoteJobStore.AppendJobTrace(jobID, epoch, chunk, offset)`. Under concurrent or retried network requests, runners may resend chunks with an already-persisted offset. `AppendJobTrace` safely acknowledges these retries without appending duplicate bytes to the database.

However, the SSE event hub broadcasting added in `handler_runner_jobs.go` blindly broadcasts every incoming chunk when `AppendJobTrace` succeeds, regardless of whether the chunk was newly appended or ignored as a retry. Additionally, `TracePayload.Offset` carries the job-level offset rather than a run-level offset, making client-side deduplication ambiguous.

## Goals / Non-Goals

**Goals:**
- Guarantee that trace events broadcast to SSE subscribers match the bytes committed to persistence.
- Prevent duplicate trace chunks in SSE broadcasts when runners retry trace appends.
- Move event broadcasting from HTTP adapters into the core service layer (`RunService`).
- Emit task `running` transitions when remote jobs are claimed.

**Non-Goals:**
- Modifying the remote runner binary or its communication protocol.
- Re-architecting local runner trace streaming.

## Decisions

### D1: Extend `RemoteJobStore.AppendJobTrace` return signature
- **Choice:** Change `AppendJobTrace` to return `(appended int, ok bool, err error)` or an `AppendResult` struct indicating the number of new bytes stored and whether the chunk was truncated.
- **Alternatives considered:** Querying `GetRemoteJob` trace offset before and after append. Rejected due to extra database queries and race conditions between concurrent appends.

### D2: Centralize remote trace publication in `RunService.AppendRemoteJobTrace`
- **Choice:** Introduce `RunService.AppendRemoteJobTrace(jobID, epoch, chunk, offset)` which delegates to the store, calculates run/job attribution, and broadcasts to `RunEventHub` only for `appended > 0`.
- **Alternatives considered:** Keeping broadcast logic in `handler_runner_jobs.go`. Rejected because web handlers are thin adapters and should not coordinate business events or database state comparisons.

### D3: Emit task transition on job claim
- **Choice:** In `RunService.ClaimNextJob` (or handler delegation), broadcast `EventTypeTask` with status `running` upon successful claim.
- **Alternatives considered:** Having the runner send an explicit start event. Rejected because job claim is already the authoritative state transition.

## Risks / Trade-offs

- **[Risk]** Interface break across multiple packages (`domain`, `store`, `service`, `web`, `mocks`).  
  → *Mitigation:* Update all implementations and mock suites in a dedicated PR with full test coverage.
