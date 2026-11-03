# ui-realtime-streaming Specification

## Purpose
Provides real-time event streaming for pipeline execution runs, task state transitions, and live log traces via Server-Sent Events (SSE).

## Requirements

### Requirement: Server-Sent Events endpoint for pipeline runs

The server SHALL expose a Server-Sent Events endpoint `GET /api/projects/{name}/runs/{id}/events` streaming `text/event-stream`. The endpoint SHALL require valid authentication with at least viewer role, emit a `: ping` heartbeat comment every 15 seconds to prevent proxy timeouts, and close the stream when the run reaches a terminal status.

#### Scenario: Subscribing to an active run stream
- **WHEN** an authenticated viewer client connects to `GET /api/projects/{name}/runs/{id}/events` for a running pipeline
- **THEN** the server SHALL return HTTP 200 with `Content-Type: text/event-stream`
- **AND** the server SHALL stream live run updates as typed Server-Sent Events

#### Scenario: Stream closes when run finishes
- **WHEN** the observed run finishes with status `passed`, `failed`, or `canceled`
- **THEN** the server SHALL emit a final complete event indicating completion
- **AND** the server SHALL close the HTTP response stream cleanly

### Requirement: Event types and payloads

The SSE stream SHALL emit structured events formatted as JSON payloads for `status`, `task`, `trace`, and `complete`. The `trace` event SHALL include `task_name`, `chunk`, and `offset`. The `task` event SHALL include `task_name`, `status`, and optional `error`. The `status` event SHALL include `run_id`, `status`, and optional `error`. The `complete` event SHALL include `run_id` and terminal `status`.

#### Scenario: Streaming live trace chunk
- **WHEN** new output is appended to an active run
- **THEN** the server SHALL broadcast a `trace` event with the task name, chunk text, and offset to all active subscribers

#### Scenario: Streaming task status transition
- **WHEN** a task transitions from `pending` to `running`, or to `passed`, `failed`, or `skipped`
- **THEN** the server SHALL broadcast a `task` event with the task name, new status, and optional error

#### Scenario: Streaming run completion
- **WHEN** a run reaches a terminal status
- **THEN** the server SHALL broadcast a `complete` event with `run_id` and terminal `status`

### Requirement: Frontend real-time consumption with polling fallback

The web frontend SHALL connect to the run events endpoint when viewing an active run. The frontend SHALL append received trace chunks and update DAG node statuses in real time. If the event stream connection fails or closes unexpectedly, the frontend SHALL automatically fall back to polling `GET /api/projects/{name}/runs/{id}` every 1.5 seconds. On opening or reconnecting the stream, the frontend SHALL resync run state from the REST endpoint.

#### Scenario: Live updates update DAG and terminal
- **WHEN** the browser receives SSE events for a running task
- **THEN** React Flow SHALL update the node status color immediately
- **AND** the log terminal viewer SHALL append incoming trace text without page reload

#### Scenario: Fallback to polling on stream failure
- **WHEN** the SSE connection fails to establish or drops unexpectedly
- **THEN** the frontend SHALL revert to periodic HTTP polling until the run completes

#### Scenario: Resync on stream connection
- **WHEN** the SSE connection opens or re-establishes
- **THEN** the frontend SHALL refetch the run snapshot to ensure no events were missed during the connection gap
