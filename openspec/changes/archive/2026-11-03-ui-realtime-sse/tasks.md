# Tasks: ui-realtime-sse

## 1. Backend Event Broker and SSE Handler

- [x] 1.1 Implement in-memory `RunEventHub` in `internal/service` with subscribe, unsubscribe, and broadcast methods; verify with unit tests in `internal/service/event_hub_test.go`
- [x] 1.2 Wire `RunEventHub` into `RunService` (task status transitions) and trace handlers (`AppendJobTrace` output events); verify events are emitted on state changes
- [x] 1.3 Implement HTTP SSE handler `GET /api/projects/{name}/runs/{id}/events` in `internal/web/handler_sse.go` with heartbeats and terminal close; verify with HTTP integration test

## 2. Frontend SSE Integration and Fallback

- [x] 2.1 Create `useRunEvents` hook in `ui/src/hooks/useRunEvents.js` using browser `EventSource` with reconnect and error fallback; verify with Vitest mock tests
- [x] 2.2 Update `Pipelines.jsx` to consume live events for real-time log terminal appending and React Flow DAG node color updates; verify component test in `ui`
- [x] 2.3 Verify transparent fallback to periodic polling when `EventSource` is closed or disconnected

## 3. End-to-End Verification

- [x] 3.1 Add an integration test driving the SSE handler over real HTTP and verifying client reception of hub-broadcast events; verify `scripts/test-all.sh` passes (see REVIEW.md R14 - this does not drive a real RunService execution, since MockPipelineRunner never calls the task start/complete callbacks)
- [x] 3.2 Run `make lint` and `make ui-lint` to ensure clean code formatting and zero static analysis warnings
