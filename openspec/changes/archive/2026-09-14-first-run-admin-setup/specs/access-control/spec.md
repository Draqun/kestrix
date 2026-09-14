## MODIFIED Requirements

### Requirement: Authentication required on API routes

Kestrix SHALL require a valid token on every `/api` route except the health,
login, logout, and first-run setup endpoints (`GET /api/setup`,
`POST /api/setup`). The token SHALL be accepted from a bearer
authorization header or from the session cookie. A request with no credentials,
a malformed authorization header, or an invalid token SHALL be rejected with
`401` before the route's handler runs. A route added later SHALL be covered by
this requirement unless it is deliberately registered as public. The setup
endpoints are public only so a fresh instance can create its first
administrator; `first-run-setup` defines how they close themselves once any
account exists.

#### Scenario: Call without credentials

- **WHEN** a request reaches any protected `/api` route with no authorization header and no session cookie
- **THEN** Kestrix SHALL reject it with `401`
- **AND** the handler SHALL NOT run

#### Scenario: Malformed authorization header

- **WHEN** a request presents an authorization header that is not a bearer token
- **THEN** Kestrix SHALL reject it with `401`
- **AND** SHALL NOT fall back to a session cookie on that request

#### Scenario: Session cookie authenticates a protected route

- **WHEN** a request reaches a protected `/api` route with a valid session cookie and no authorization header
- **THEN** Kestrix SHALL authenticate it and run the route's authorization

#### Scenario: Health and login stay reachable

- **WHEN** a request reaches the health or login endpoint without credentials
- **THEN** Kestrix SHALL serve it

#### Scenario: Logout stays reachable

- **WHEN** a request reaches the logout endpoint without credentials
- **THEN** Kestrix SHALL serve it

#### Scenario: Setup endpoints stay reachable

- **WHEN** a request reaches `GET /api/setup` or `POST /api/setup` without credentials
- **THEN** Kestrix SHALL serve it according to `first-run-setup` rather than reject it with `401`
