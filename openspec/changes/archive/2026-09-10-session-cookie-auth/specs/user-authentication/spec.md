## MODIFIED Requirements

### Requirement: Token issuance on login

Kestrix SHALL expose an unauthenticated login endpoint that accepts a name and
password and, on success, returns a signed token carrying the account name, the
account's current token version, and an expiry. The response SHALL also state
when the token expires. Kestrix SHALL reject a login with an unknown name and a
login with a wrong password identically, disclosing neither which one failed nor
whether the account exists.

A token SHALL NOT be the authority on what the account may do. Any role the
token carries is informational for the interface only, and Kestrix SHALL NOT use
it to decide a request.

On success the response SHALL also set a session cookie carrying the same
token. The cookie SHALL be marked `HttpOnly` and `SameSite=Strict`, SHALL be
scoped to the API path, SHALL expire when the token expires, and SHALL be
marked `Secure` unless the deployment explicitly disables that mark for
plain-HTTP development. The cookie SHALL NOT be readable by script running in
the page.

#### Scenario: Successful login

- **WHEN** a request presents a name and the matching password
- **THEN** Kestrix SHALL return a signed token and its expiry
- **AND** the response SHALL NOT contain the password hash

#### Scenario: Successful login sets a session cookie

- **WHEN** a request presents a name and the matching password
- **THEN** the response SHALL set a session cookie carrying the token
- **AND** the cookie SHALL be marked `HttpOnly`, `SameSite=Strict`, and `Secure`
- **AND** the cookie's lifetime SHALL equal the token's remaining lifetime

#### Scenario: Secure mark disabled for development

- **WHEN** the deployment has explicitly disabled the secure cookie mark
- **AND** a request presents valid credentials
- **THEN** the session cookie SHALL still be marked `HttpOnly` and `SameSite=Strict`
- **AND** SHALL NOT be marked `Secure`

#### Scenario: Wrong password

- **WHEN** a request presents an existing name and a wrong password
- **THEN** Kestrix SHALL reject it with `401`
- **AND** the error SHALL be identical to the error for an unknown account
- **AND** the response SHALL NOT set a session cookie

#### Scenario: Unknown account

- **WHEN** a request presents a name that does not exist
- **THEN** Kestrix SHALL reject it with `401`
- **AND** SHALL perform password verification work equivalent to an existing account

#### Scenario: Token carries the current version

- **WHEN** Kestrix issues a token for an account
- **THEN** the token SHALL carry that account's stored token version at the moment of issue

### Requirement: Token verification

Kestrix SHALL accept a token only when its signature verifies against the
configured signing key, its signing algorithm is the expected one, it has not
expired, it names an account that still exists, and it carries a token version
equal to the version stored for that account. Kestrix SHALL reject a token whose
header names a different algorithm, including an unsigned one, even when the
rest of the token is well-formed. A token carrying no token version SHALL be
rejected.

Every rejection under this requirement SHALL be `401` and SHALL carry the same
message, so that a caller cannot tell a bad signature from an unknown account
from a superseded version.

Kestrix SHALL read the token from the bearer authorization header when one is
present, and otherwise from the session cookie. When both are present the
header SHALL be used and the cookie ignored. A token presented in the cookie
SHALL be verified by exactly the same rules as one presented in the header,
including token-version revocation, and a rejected cookie token SHALL answer
with the same status and body as a rejected header token.

#### Scenario: Expired token

- **WHEN** a request presents a token whose expiry has passed
- **THEN** Kestrix SHALL reject the request with `401`

#### Scenario: Token signed with another key

- **WHEN** a request presents a token whose signature does not verify against the configured key
- **THEN** Kestrix SHALL reject the request with `401`

#### Scenario: Algorithm substitution is refused

- **WHEN** a request presents a token declaring an unsigned or unexpected signing algorithm
- **THEN** Kestrix SHALL reject the request with `401`
- **AND** SHALL NOT read any claim from that token

#### Scenario: Token for a deleted account

- **WHEN** a request presents an unexpired, correctly signed token naming an account that no longer exists
- **THEN** Kestrix SHALL reject the request with `401`

#### Scenario: Token without a version claim

- **WHEN** a request presents an unexpired, correctly signed token that carries no token version
- **THEN** Kestrix SHALL reject the request with `401`

#### Scenario: Rejections are indistinguishable

- **WHEN** Kestrix rejects a bad signature, an unknown account, and a superseded token version
- **THEN** all three responses SHALL carry the same status and the same message

#### Scenario: Valid token in the session cookie

- **WHEN** a request carries a valid token in the session cookie and no authorization header
- **THEN** Kestrix SHALL authenticate the request as that account

#### Scenario: Header takes precedence over cookie

- **WHEN** a request carries a valid token in the authorization header and a different token in the session cookie
- **THEN** Kestrix SHALL authenticate using the header token only
- **AND** the cookie token's validity SHALL NOT affect the outcome

#### Scenario: Revoked token in the cookie

- **WHEN** a request carries a session cookie whose token version has been superseded
- **THEN** Kestrix SHALL reject the request with `401`
- **AND** the response SHALL be identical to the rejection of the same token in a header

## ADDED Requirements

### Requirement: Logout ends the cookie session

Kestrix SHALL expose an unauthenticated logout endpoint that instructs the
browser to discard the session cookie. It SHALL succeed whether or not a valid
session is present, and SHALL NOT change the account's token version or affect
any bearer client holding the same token.

#### Scenario: Signed-in browser logs out

- **WHEN** a request with a valid session cookie calls the logout endpoint
- **THEN** the response SHALL clear the session cookie
- **AND** a subsequent request carrying no credentials SHALL be rejected with `401`

#### Scenario: Logout without a session

- **WHEN** a request with no credentials calls the logout endpoint
- **THEN** Kestrix SHALL respond successfully
- **AND** SHALL clear the session cookie

#### Scenario: Logout does not revoke the token

- **WHEN** a browser logs out while a bearer client holds a token for the same account
- **THEN** the bearer client's token SHALL remain valid until it expires

### Requirement: Current session endpoint

Kestrix SHALL expose an authenticated endpoint returning the signed-in
account's name, current global role, and token expiry, so that a client that
cannot read its own credential can restore its view of the session. The
response SHALL NOT contain the token or any password hash.

#### Scenario: Session is restored from the cookie

- **WHEN** a request with a valid session cookie calls the current-session endpoint
- **THEN** Kestrix SHALL return the account name, its stored global role, and the token expiry
- **AND** the response SHALL NOT contain the token

#### Scenario: No session to restore

- **WHEN** a request with no credentials calls the current-session endpoint
- **THEN** Kestrix SHALL reject it with `401`

### Requirement: Cookie sessions prove same-origin intent

A request authenticated by the session cookie whose method may change state
SHALL carry a designated custom request header. A cookie-authenticated
state-changing request without that header SHALL be rejected with `403` before
the route's handler runs, and the rejection SHALL NOT change any state. Requests
authenticated by the bearer header SHALL NOT be subject to this check, and
read-only methods SHALL NOT be subject to it.

#### Scenario: Cookie request without the header

- **WHEN** a request with a valid session cookie and no bearer header sends a state-changing method without the designated header
- **THEN** Kestrix SHALL reject it with `403`
- **AND** the handler SHALL NOT run

#### Scenario: Cookie request with the header

- **WHEN** a request with a valid session cookie sends a state-changing method with the designated header
- **THEN** Kestrix SHALL authenticate it and continue to authorization

#### Scenario: Cookie read request needs no header

- **WHEN** a request with a valid session cookie sends a read-only method without the designated header
- **THEN** Kestrix SHALL authenticate it and continue to authorization

#### Scenario: Bearer request is exempt

- **WHEN** a request with a valid bearer header sends a state-changing method without the designated header
- **THEN** Kestrix SHALL authenticate it and continue to authorization
