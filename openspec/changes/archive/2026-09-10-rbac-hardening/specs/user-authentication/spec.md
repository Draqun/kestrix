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

#### Scenario: Successful login

- **WHEN** a request presents a name and the matching password
- **THEN** Kestrix SHALL return a signed token and its expiry
- **AND** the response SHALL NOT contain the password hash

#### Scenario: Wrong password

- **WHEN** a request presents an existing name and a wrong password
- **THEN** Kestrix SHALL reject it with `401`
- **AND** the error SHALL be identical to the error for an unknown account

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

## ADDED Requirements

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
