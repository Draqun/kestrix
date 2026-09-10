## Purpose

Bounds what a single HTTP client can make the Kestrix process hold: connection lifetime, header and body size, and the time a request may take to arrive or be answered.

## ADDED Requirements

### Requirement: Listener timeouts

The HTTP listener SHALL enforce a read timeout covering request headers and body, a write timeout covering the response, and an idle timeout for keep-alive connections. Each SHALL have a non-zero default and SHALL be configurable through the canonical `KESTRIX_HTTP_READ_TIMEOUT`, `KESTRIX_HTTP_WRITE_TIMEOUT`, and `KESTRIX_HTTP_IDLE_TIMEOUT` settings as Go duration strings. A non-positive or unparseable value SHALL fail startup with a message naming the setting. The write timeout default SHALL be long enough for a webhook delivery that acquires a remote source before responding.

#### Scenario: Slow client is disconnected

- **WHEN** a client opens a connection and sends request bytes slower than the read timeout allows
- **THEN** the server SHALL close the connection once the read timeout elapses
- **AND** no handler SHALL run

#### Scenario: Idle keep-alive connection is closed

- **WHEN** a client completes a request and sends nothing further for longer than the idle timeout
- **THEN** the server SHALL close the connection

#### Scenario: Invalid timeout fails startup

- **WHEN** `KESTRIX_HTTP_READ_TIMEOUT` is set to `0`, a negative duration, or a non-duration string
- **THEN** the server SHALL refuse to start
- **AND** the error SHALL name `KESTRIX_HTTP_READ_TIMEOUT`

#### Scenario: Defaults apply when unset

- **WHEN** none of the timeout settings is provided
- **THEN** the server SHALL start with non-zero read, write, and idle timeouts

### Requirement: Bounded request bodies

Every request body the server reads SHALL be bounded. API routes SHALL share one bound configured by `KESTRIX_HTTP_MAX_BODY_BYTES`; `POST /webhook` SHALL have its own bound configured by `KESTRIX_WEBHOOK_MAX_BODY_BYTES`, since a push payload carries file lists for up to twenty commits. Both SHALL have positive defaults. A request whose body exceeds its bound SHALL be answered `413` with the standard JSON error shape, SHALL NOT run the route's business logic, and SHALL be handled identically whether the body is read as JSON or as raw bytes. A non-positive or unparseable bound SHALL fail startup with a message naming the setting.

#### Scenario: Oversized API body

- **WHEN** an authenticated client sends `POST /api/projects` with a body larger than `KESTRIX_HTTP_MAX_BODY_BYTES`
- **THEN** the server SHALL respond `413`
- **AND** no project SHALL be created

#### Scenario: Oversized login body

- **WHEN** a client sends `POST /api/auth/login` with a body larger than `KESTRIX_HTTP_MAX_BODY_BYTES`
- **THEN** the server SHALL respond `413`
- **AND** the login throttle and password verification SHALL NOT run

#### Scenario: Body within bound is unaffected

- **WHEN** a client sends a request whose body is within its bound
- **THEN** the route SHALL behave exactly as it did before the bound existed

#### Scenario: Webhook bound is independent

- **WHEN** `KESTRIX_WEBHOOK_MAX_BODY_BYTES` is larger than `KESTRIX_HTTP_MAX_BODY_BYTES`
- **AND** a delivery to `POST /webhook` has a body between the two values
- **THEN** the server SHALL accept and verify the delivery

#### Scenario: Invalid bound fails startup

- **WHEN** `KESTRIX_HTTP_MAX_BODY_BYTES` is set to `0`, a negative number, or a non-integer
- **THEN** the server SHALL refuse to start
- **AND** the error SHALL name `KESTRIX_HTTP_MAX_BODY_BYTES`
