# user-authentication Specification

## Purpose
Establishes who is making a request: local user accounts, password
verification, and the signed tokens that carry an identity between the login
call and every later API call.

## Requirements

### Requirement: Local user accounts

Kestrix SHALL store user accounts in PostgreSQL with a unique name, a password
hash, a global role, and creation and update timestamps. Kestrix SHALL NOT
store the password itself, and SHALL NOT return a password hash from any API
response or CLI output.

#### Scenario: Account is created

- **WHEN** an operator creates an account with a name that is not yet taken
- **THEN** Kestrix SHALL persist the account with the requested global role
- **AND** the stored record SHALL contain a password hash rather than the password

#### Scenario: Duplicate account name is rejected

- **WHEN** an operator creates an account whose name already exists
- **THEN** Kestrix SHALL reject the request
- **AND** SHALL leave the existing account unchanged

### Requirement: Password hashing

Kestrix SHALL hash passwords with a memory-hard algorithm using a per-account
random salt, and SHALL record the algorithm parameters alongside the hash so
that they can be raised later without invalidating stored hashes. Password
comparison SHALL NOT be sensitive to timing.

#### Scenario: Two accounts share a password

- **WHEN** two accounts are created with the same password
- **THEN** their stored hashes SHALL differ

#### Scenario: Stored parameters are honored

- **WHEN** a hash records parameters that differ from the current defaults
- **THEN** verification SHALL use the parameters recorded in that hash
- **AND** a correct password SHALL still authenticate

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

### Requirement: Configured token signing key

Kestrix SHALL read the token signing key from the file named by
`KESTRIX_JWT_SIGNING_KEY_PATH` and SHALL validate that file before the HTTP
listener accepts requests: it must exist, must not be readable by group or
other users, and must contain a 256-bit key as 32 raw bytes or 64 hexadecimal
characters. Kestrix SHALL NOT derive this key from the credential master key,
and SHALL NOT generate one at startup. Errors SHALL identify the configuration
or file condition without exposing key material.

#### Scenario: Signing key is not configured

- **WHEN** Kestrix starts without a non-empty `KESTRIX_JWT_SIGNING_KEY_PATH`
- **THEN** startup SHALL fail before the HTTP listener is available
- **AND** the error SHALL name the missing variable without exposing key material

#### Scenario: Signing key file is unusable

- **WHEN** the configured path is missing, permissive, unreadable, or malformed
- **THEN** Kestrix SHALL fail startup before serving HTTP
- **AND** no key bytes SHALL appear in the error or logs

#### Scenario: Tokens survive a restart

- **WHEN** Kestrix restarts with the same signing key file
- **THEN** a token issued before the restart and not yet expired SHALL still be accepted

### Requirement: Account management from the command line

Kestrix SHALL provide command-line account management that operates directly
against the configured database, without requiring a reachable API or an
existing account: creating an account with a chosen global role, listing
accounts, changing a password, and deleting an account. The command SHALL read
the password without echoing it and SHALL NOT accept it as a plain command-line
argument.

#### Scenario: First administrator is created

- **WHEN** an operator runs account creation with the administrator role against an empty database
- **THEN** Kestrix SHALL create that account
- **AND** the operator SHALL be able to log in with it

#### Scenario: Password is not exposed to the process table

- **WHEN** an operator creates or updates an account
- **THEN** the password SHALL be read interactively or from standard input
- **AND** SHALL NOT be passed as a command-line argument

#### Scenario: Listing accounts

- **WHEN** an operator lists accounts
- **THEN** the output SHALL include each name, its global role, and its timestamps
- **AND** SHALL NOT include any password hash

### Requirement: Server never runs unauthenticated

Kestrix SHALL NOT provide a mode in which the API is served without
authentication, and SHALL NOT create an account automatically at startup. An
empty account table SHALL leave the API reachable but unusable until an
operator creates an account with the command-line tool.

#### Scenario: No accounts exist

- **WHEN** Kestrix serves requests while the account table is empty
- **THEN** every authenticated route SHALL reject requests with `401`
- **AND** Kestrix SHALL NOT create a default or fallback account

### Requirement: Session revocation

Kestrix SHALL store a token version for every account and SHALL increment it
whenever the account's password is changed, its global role is changed, or the
account is deleted. Incrementing the version SHALL make every token issued
before the increment unusable on the next request that presents it, without
waiting for the token to expire and without changing the signing key.

#### Scenario: Password change ends existing sessions

- **WHEN** an account's password is changed
- **AND** a request then presents a token issued before the change
- **THEN** Kestrix SHALL reject the request with `401`

#### Scenario: Role change ends existing sessions

- **WHEN** an administrator changes an account's global role
- **AND** a request then presents a token issued before the change
- **THEN** Kestrix SHALL reject the request with `401`

#### Scenario: Other accounts are unaffected

- **WHEN** one account's token version is incremented
- **THEN** unexpired tokens held by other accounts SHALL continue to be accepted

#### Scenario: A new token works immediately

- **WHEN** an account whose version was incremented logs in again
- **THEN** the newly issued token SHALL be accepted

### Requirement: Login throttling

Kestrix SHALL limit repeated failed logins by account name and by client
address, and SHALL reject an attempt beyond the configured limit within the
configured window with `429` and a `Retry-After` header. A throttled attempt
SHALL NOT reach password verification, so that the cost of verification cannot
be used to amplify an attack. A successful login SHALL clear that account's
failure count. Throttling SHALL be applied identically to a name that exists and
one that does not.

#### Scenario: Repeated failures on one account

- **WHEN** the configured number of failed logins for one account name occurs within the configured window
- **THEN** Kestrix SHALL reject the next attempt with `429`
- **AND** the response SHALL carry a `Retry-After` header

#### Scenario: Failures spread across accounts from one client

- **WHEN** one client address exceeds the configured number of failed logins across different account names
- **THEN** Kestrix SHALL reject its next attempt with `429`

#### Scenario: Throttling does not disclose account existence

- **WHEN** failed attempts accumulate against a name that does not exist
- **THEN** Kestrix SHALL throttle it exactly as it throttles an existing name

#### Scenario: Success clears the counter

- **WHEN** an account authenticates successfully after some failed attempts within the window
- **THEN** its failure count SHALL be cleared

#### Scenario: Throttled attempt does no verification work

- **WHEN** an attempt is rejected by the throttle
- **THEN** Kestrix SHALL NOT perform password hashing or verification for it

### Requirement: Authentication and authorization decisions are logged

Kestrix SHALL log every rejected authentication, every throttled login, and
every authorization denial, recording the account name where one is known, the
client address, the route, and the reason. These records SHALL NOT contain the
token, any part of it, the password, the password hash, or the request body.

#### Scenario: Failed login is recorded

- **WHEN** a login is rejected
- **THEN** Kestrix SHALL log the attempted account name, the client address, and that authentication failed
- **AND** the record SHALL NOT contain the submitted password

#### Scenario: Denial is recorded

- **WHEN** a request is rejected with `403`
- **THEN** Kestrix SHALL log the account name, the route, the required role, and the effective role

#### Scenario: Tokens never reach the log

- **WHEN** any authentication or authorization record is written
- **THEN** it SHALL NOT contain the bearer token or any substring of it

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

### Requirement: Signing key rotation

Replacing the configured token signing key and restarting Kestrix SHALL be
the supported way to rotate it. After the restart every token signed with
the previous key SHALL be rejected as invalid, so every session ends, while
runner tokens and the metrics token SHALL keep working. The deployment
documentation SHALL describe this procedure and SHALL list it in a checklist
for a suspected key leak, together with master key rotation.

#### Scenario: Old sessions end after rotation
- **WHEN** an operator replaces the signing key file and restarts Kestrix
- **THEN** a token issued before the restart SHALL be rejected with `401`
- **AND** signing in again SHALL issue a token that is accepted

#### Scenario: Runners unaffected
- **WHEN** the signing key is rotated
- **THEN** a registered runner SHALL keep authenticating with its existing token

