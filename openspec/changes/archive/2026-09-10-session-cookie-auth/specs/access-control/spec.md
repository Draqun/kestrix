## MODIFIED Requirements

### Requirement: Authentication required on API routes

Kestrix SHALL require a valid token on every `/api` route except the health,
login, and logout endpoints. The token SHALL be accepted from a bearer
authorization header or from the session cookie. A request with no credentials,
a malformed authorization header, or an invalid token SHALL be rejected with
`401` before the route's handler runs. A route added later SHALL be covered by
this requirement unless it is deliberately registered as public.

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

### Requirement: Interface reflects authentication and role

The user interface SHALL require a login before showing application data, SHALL
rely on the session cookie rather than holding the token in script-readable
storage, SHALL send the designated same-origin header on every API call, and
SHALL return the user to the login view when the API reports that
authentication failed. On load, the interface SHALL restore the signed-in
account's name and role from the current-session endpoint rather than from a
stored token. The interface SHALL NOT offer actions the current effective role
cannot perform.

#### Scenario: Unauthenticated visitor

- **WHEN** a visitor with no valid session opens any application view
- **THEN** the interface SHALL show the login view
- **AND** SHALL NOT display project, run, or credential data

#### Scenario: Token is not exposed to script

- **WHEN** a user has signed in through the interface
- **THEN** the token SHALL NOT be present in `localStorage`, `sessionStorage`, or any script-readable cookie

#### Scenario: Session survives a reload

- **WHEN** a signed-in user reloads the page before the session expires
- **THEN** the interface SHALL restore the account name and role from the current-session endpoint
- **AND** SHALL NOT show the login view

#### Scenario: Session ends while the interface is open

- **WHEN** an API call reports that authentication failed
- **THEN** the interface SHALL discard its view of the session and return to the login view

#### Scenario: Sign-out clears the cookie

- **WHEN** a signed-in user signs out
- **THEN** the interface SHALL call the logout endpoint
- **AND** SHALL show the login view

#### Scenario: Viewer sees no mutating actions

- **WHEN** an account whose effective role is `viewer` views a project
- **THEN** the interface SHALL NOT offer run, edit, or delete actions for it

#### Scenario: Hidden actions are still refused by the API

- **WHEN** a request for a hidden action reaches the API anyway
- **THEN** Kestrix SHALL reject it with `403`
