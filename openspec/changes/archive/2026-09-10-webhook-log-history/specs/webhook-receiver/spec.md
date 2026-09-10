## ADDED Requirements

### Requirement: Event Recording

The webhook receiver SHALL record every event it parses to durable webhook history before responding, including events that are ignored because no registered project matches, resolved without triggering tasks, or failed during processing. A request rejected before its payload is parsed SHALL NOT be recorded, so that an unauthenticated caller cannot evict history through the retention bound. A recording failure SHALL NOT change the receiver's response to the caller.

#### Scenario: Ignored event still recorded

- **WHEN** the receiver rejects an event because no registered project matches its GitLab project ID
- **THEN** the event SHALL be recorded with status `ignored`
- **AND** the caller SHALL still receive the ignored response

#### Scenario: Resolved event with no triggered tasks

- **WHEN** the receiver resolves an event and no task is triggered
- **THEN** the event SHALL be recorded with status `skipped`

#### Scenario: Unparsed request not recorded

- **WHEN** a request is rejected because its token does not match, its body cannot be read, or its payload is not a GitLab push event
- **THEN** no webhook event SHALL be recorded

#### Scenario: Recording failure does not fail the request

- **WHEN** the history store is unavailable while an event is processed
- **THEN** the receiver SHALL still return its normal response
- **AND** the failure SHALL be logged

## MODIFIED Requirements

### Requirement: HTTP Endpoint (implemented)

The server SHALL expose a `POST /webhook` endpoint that accepts JSON payloads from GitLab push webhooks. The server SHALL also expose a `GET /api/health` endpoint that reports service status and stored counts, and a `GET /ping` heartbeat, for monitoring.

#### Scenario: Health check

- **WHEN** a client sends `GET /api/health` and every backing store answers
- **THEN** the server SHALL respond with `200 OK` and JSON carrying `status` `"ok"`, `service`, `repos`, the stored webhook event count and the configured project count

#### Scenario: Health check with a failing store

- **WHEN** a client sends `GET /api/health` and a backing store query fails
- **THEN** the server SHALL respond with `503 Service Unavailable`
- **AND** the body SHALL carry `status` `"degraded"` and SHALL omit the counter whose query failed

#### Scenario: Heartbeat

- **WHEN** a client sends `GET /ping`
- **THEN** the server SHALL respond with `200 OK` and the plain text `.`
