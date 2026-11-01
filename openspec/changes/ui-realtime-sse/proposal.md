# Proposal: ui-realtime-sse

## Why

Currently, the React Flow pipeline interface (`ui/src/pages/Pipelines.jsx`) polls `GET /api/projects/{name}/runs/{id}` every 1.5 seconds while a run is active. This continuous short-polling incurs unnecessary database reads, adds HTTP overhead, and produces jerky terminal log rendering. A push-based Server-Sent Events (SSE) stream delivers instantaneous live log output and immediate node status transitions with reduced server load.

## What Changes

- Add a server-side SSE endpoint: `GET /api/projects/{name}/runs/{id}/events` (streaming `text/event-stream`).
- The stream emits typed events for active runs:
  - `status`: overall run status transitions (`running`, `passed`, `failed`, `canceled`).
  - `task`: task status transitions (`pending`, `running`, `passed`, `failed`, `skipped`) and details.
  - `trace`: new log chunks appended with byte offsets.
- When an active run reaches a terminal state, the stream sends a final `complete` event and closes cleanly.
- An in-process pub/sub event broker broadcasts run updates from `RunService` and trace handlers to active SSE subscribers.
- The web frontend (`Pipelines.jsx`) connects via `EventSource`:
  - Appends incoming trace chunks to the terminal viewer in real time.
  - Transitions React Flow node border colors and status icons immediately upon event receipt.
  - Falls back to 1.5-second polling if SSE is disconnected, blocked by an intermediate proxy, or unsupported.

## Capabilities

### New Capabilities

- `ui-realtime-streaming`: Push-based Server-Sent Events (SSE) stream for run status, task transitions, and live logs.

## Non-goals

- Bidirectional WebSockets (SSE is unidirectional and natively supported over standard HTTP/1.1 and HTTP/2).
- Client-to-server messaging over the event stream.
- Replacing the primary REST API for historical runs (completed runs continue using standard REST endpoints).

## Impact

- `internal/web`: Add SSE handler `GET /api/projects/{name}/runs/{id}/events` with keepalive heartbeats.
- `internal/service`: Add run event hub / broadcaster notifying listeners on task state changes and trace appends.
- `ui/src`: Add `useRunEvents` hook connecting to SSE with automatic polling fallback.
