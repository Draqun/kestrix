## MODIFIED Requirements

### Requirement: HTTP Endpoint (implemented)

The server SHALL expose a `POST /webhook` endpoint that accepts JSON payloads from GitLab push webhooks. The server SHALL also expose a `GET /api/health` endpoint that reports service status and stored counts, and a `GET /ping` heartbeat, for monitoring.

Every delivery to `POST /webhook` SHALL be authenticated before any part of its payload is trusted, and verification SHALL fail closed. A delivery is accepted only if it verifies, using the configured method (`gitlab` header comparison or `hmac` body signature), against either the matched registered project's own webhook secret or the global webhook secret. A delivery that verifies against neither, including one for which no secret is configured at all, SHALL be rejected with `401`, SHALL NOT be recorded in webhook history, and SHALL NOT trigger any pipeline load, source acquisition, resolution, or dispatch. Secret comparison SHALL be constant time. The rejection response SHALL be identical regardless of whether the project is unknown, has no secret, or presented a wrong secret, and SHALL NOT disclose which secret was consulted.

The request body SHALL be read at most once and SHALL be bounded by the configured webhook body limit; a body exceeding it SHALL be rejected with `413` and SHALL NOT be recorded.

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

#### Scenario: Delivery verified by the project's own secret

- **WHEN** a push event arrives for a registered project that has a webhook secret
- **AND** the delivery verifies against that secret
- **THEN** the server SHALL process the event as before

#### Scenario: Delivery verified by the global secret

- **WHEN** a push event arrives for a registered project that has no webhook secret of its own
- **AND** the global webhook secret is configured and the delivery verifies against it
- **THEN** the server SHALL process the event as before

#### Scenario: No secret configured anywhere

- **WHEN** a push event arrives and neither the matched project nor the global configuration has a webhook secret
- **THEN** the server SHALL respond `401`
- **AND** SHALL NOT record the event, load a pipeline, acquire a source, or dispatch

#### Scenario: Wrong secret

- **WHEN** a push event presents a token or signature that verifies against neither the project's secret nor the global secret
- **THEN** the server SHALL respond `401`
- **AND** SHALL NOT record the event

#### Scenario: Unknown project does not learn anything

- **WHEN** a push event names a GitLab project ID that is not registered and does not verify against the global secret
- **THEN** the server SHALL respond `401` with a body and status byte-identical to the wrong-secret rejection

#### Scenario: Unknown project with a valid global secret is still ignored

- **WHEN** a push event names an unregistered GitLab project ID and verifies against the global secret
- **THEN** the server SHALL respond `200` with `{"status": "ignored", "reason": "unknown project"}` and record the event with status `ignored`, as before

#### Scenario: HMAC method is honoured

- **WHEN** the verification method is `hmac`
- **AND** a delivery carries an `X-Hub-Signature-256` header whose HMAC-SHA256 over the raw body matches the applicable secret
- **THEN** the server SHALL accept the delivery
- **AND** a delivery carrying only a matching `X-Gitlab-Token` header SHALL be rejected with `401`

#### Scenario: Oversized delivery

- **WHEN** a delivery's body exceeds the configured webhook body limit
- **THEN** the server SHALL respond `413`
- **AND** SHALL NOT record the event

#### Scenario: Verification precedes parsing

- **WHEN** a delivery fails verification and its body is also not a valid GitLab push event
- **THEN** the server SHALL respond `401`, not `400`
