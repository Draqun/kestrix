# Tasks: ui-realtime-sse

## 1. Backend Event Broker and SSE Handler

- [ ] 1.1 Implement in-memory `RunEventHub` in `internal/service` with subscribe, unsubscribe, and broadcast methods; verify with unit tests in `internal/service/event_hub_test.go`
- [ ] 1.2 Wire `RunEventHub` into `RunService` (task status transitions) and trace handlers (`AppendJobTrace` output events); verify events are emitted on state changes
- [ ] 1.3 Implement HTTP SSE handler `GET /api/projects/{name}/runs/{id}/events` in `internal/web/handler_sse.go` with heartbeats and terminal close; verify with HTTP integration test

## 2. Frontend SSE Integration and Fallback

- [ ] 2.1 Create `useRunEvents` hook in `ui/src/hooks/useRunEvents.js` using browser `EventSource` with reconnect and error fallback; verify with Vitest mock tests
- [ ] 2.2 Update `Pipelines.jsx` to consume live events for real-time log terminal appending and React Flow DAG node color updates; verify component test in `ui`
- [ ] 2.3 Verify transparent fallback to periodic polling when `EventSource` is closed or disconnected

## 3. End-to-End Verification

- [ ] 3.1 Run end-to-end integration test streaming task execution and verifying SSE event reception; verify `scripts/test-all.sh` passes
- [ ] 3.2 Run `make lint` and `make ui-lint` to ensure clean code formatting and zero static analysis warnings
