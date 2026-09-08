## Purpose

Establishes who is making a request: local user accounts, password
verification, and the signed tokens that carry an identity between the login
call and every later API call.

## ADDED Requirements

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
password and, on success, returns a signed token carrying the account name, its
global role, and an expiry. The response SHALL also state when the token
expires. Kestrix SHALL reject a login with an unknown name and a login with a
wrong password identically, disclosing neither which one failed nor whether the
account exists.

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

### Requirement: Token verification

Kestrix SHALL accept a token only when its signature verifies against the
configured signing key, its signing algorithm is the expected one, and it has
not expired. Kestrix SHALL reject a token whose header names a different
algorithm, including an unsigned one, even when the rest of the token is
well-formed.

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
