## MODIFIED Requirements

### Requirement: Authorization on every route

Kestrix SHALL map every protected route to a minimum required role and SHALL
reject a request whose effective role is below it with `403`, distinct from the
`401` returned for a failed authentication. Read routes SHALL require `viewer`,
run and pipeline-editing routes SHALL require `operator`, and account,
credential, and project lifecycle routes SHALL require `admin`.

The effective role SHALL be the higher of the account's current stored global
role and its current stored grant on the route's project. Both parts SHALL be
resolved from the store on the request being authorized, never from the token
and never from a request header, query parameter, or body field. Kestrix MAY
serve either part from a cache whose maximum age is a configured bound, and
SHALL invalidate a cached entry when the account it describes is changed by
this process.

#### Scenario: Insufficient role is distinguishable from missing credentials

- **WHEN** an authenticated account calls a route above its effective role
- **THEN** Kestrix SHALL respond `403`
- **AND** an unauthenticated call to the same route SHALL respond `401`

#### Scenario: Caller-supplied role is ignored

- **WHEN** a request carries a role in a header, query parameter, or body alongside a valid token
- **THEN** Kestrix SHALL evaluate permission using only the stored global role and grant

#### Scenario: Role in the token is ignored

- **WHEN** a request presents a valid token whose role claim is higher than the account's stored global role
- **THEN** Kestrix SHALL evaluate permission using the stored role
- **AND** SHALL reject the request with `403` if the stored role is insufficient

#### Scenario: Demotion invalidates the token, not just the route

- **WHEN** an administrator lowers an account's global role
- **AND** that account calls any route with a token issued before the change
- **THEN** Kestrix SHALL reject the request with `401` within the configured cache bound
- **AND** SHALL NOT wait for the token to expire
- **AND** SHALL NOT evaluate the request against either the old or the new role

#### Scenario: Manual approval requires operator

- **WHEN** a global `viewer` approves a manual task
- **THEN** Kestrix SHALL reject the request with `403`

### Requirement: Account administration API

Kestrix SHALL expose administrator-only endpoints to list accounts, create an
account, change an account's global role, set an account's project grants, and
delete an account. Responses SHALL carry the account name, global role, grants,
and timestamps, and SHALL NOT carry password hashes.

A change to an account's global role, grants, or password, and the deletion of
an account, SHALL take effect on that account's subsequent requests without
waiting for the existing token to expire. A change to global role or password,
and account deletion, SHALL do so by ending sessions issued before the change
(see requirement "Session revocation"): the account authenticates again to
obtain a token usable at its new role. A change to project grants takes effect
without ending any session, since a grant is resolved fresh on every
project-scoped request rather than carried in the token.

#### Scenario: Non-administrator is refused

- **WHEN** an account whose global role is `viewer` or `operator` lists or modifies accounts
- **THEN** Kestrix SHALL reject the request with `403`

#### Scenario: Administrator changes a role

- **WHEN** an administrator changes another account's global role
- **AND** that account then presents a token issued before the change
- **THEN** Kestrix SHALL reject the request with `401`
- **AND** a token the account obtains by authenticating again SHALL be evaluated against the new role

#### Scenario: Deleted account loses access at once

- **WHEN** an administrator deletes an account that holds an unexpired token
- **THEN** the next request presenting that token SHALL be rejected with `401`

#### Scenario: Revoked grant stops applying

- **WHEN** an administrator removes an account's grant on a project
- **THEN** a subsequent request from that account scoped to that project SHALL be evaluated without the grant

## ADDED Requirements

### Requirement: Authorization data failures fail closed

When Kestrix cannot read the account or grant data an authorization decision
needs, it SHALL reject the request with `503` and SHALL NOT decide the request
on partial data. A store failure SHALL NOT be reported as `403`, SHALL NOT be
treated as an absent grant, and SHALL NOT reduce the caller's effective role.
The underlying error SHALL be logged and SHALL NOT be returned to the caller.

#### Scenario: Grant lookup fails

- **WHEN** the grant store returns an error while a project-scoped request is being authorized
- **THEN** Kestrix SHALL respond `503`
- **AND** SHALL NOT run the route's handler
- **AND** SHALL NOT authorize the request on the global role alone

#### Scenario: Identity lookup fails

- **WHEN** the user store returns an error while a request's account is being resolved
- **THEN** Kestrix SHALL respond `503`
- **AND** SHALL NOT authorize the request on the token's claims

#### Scenario: Absent grant is not a failure

- **WHEN** the grant store reports that no grant exists for the account and project
- **THEN** Kestrix SHALL continue with the account's global role
- **AND** SHALL NOT respond `503`

#### Scenario: Store error is not disclosed

- **WHEN** Kestrix rejects a request because an authorization store failed
- **THEN** the response body SHALL NOT contain the underlying error, the query, or the connection details

### Requirement: Cross-project read visibility

Any authenticated account SHALL be able to read every project, run, webhook
event, and dashboard aggregate that its global role permits, whether or not it
holds a grant on the project concerned. Kestrix SHALL NOT filter a listing by
the caller's grants, because a grant raises an effective role and never lowers
one. Write, run, and lifecycle actions remain governed by the effective role for
the project concerned.

#### Scenario: Viewer lists every project

- **WHEN** a global `viewer` holding no grants lists projects
- **THEN** Kestrix SHALL return every project
- **AND** SHALL NOT reduce the list to projects the account holds a grant on

#### Scenario: Reading does not imply acting

- **WHEN** that same account starts a run on a project it can read but holds no `operator` role or grant for
- **THEN** Kestrix SHALL reject the request with `403`

### Requirement: Pipeline directory access is bounded

Where a route accepts a pipeline directory from the caller, Kestrix SHALL
resolve that directory, follow any symbolic links in it, and require the result
to be the configured pipeline root or a directory below it. A directory outside
that root SHALL be refused with `403` and SHALL NOT be read, parsed, or named
back to the caller. An omitted directory SHALL continue to mean the configured
pipeline root.

#### Scenario: Directory outside the root

- **WHEN** an authenticated account requests a pipeline directory that resolves outside the configured pipeline root
- **THEN** Kestrix SHALL reject the request with `403`
- **AND** SHALL NOT read any file at that path

#### Scenario: Traversal segments do not escape

- **WHEN** a requested directory reaches outside the root through relative segments or a symbolic link
- **THEN** Kestrix SHALL reject the request with `403`

#### Scenario: Directory inside the root

- **WHEN** an authenticated account requests a directory below the configured pipeline root
- **THEN** Kestrix SHALL serve the request as before

#### Scenario: Refusal is not a path oracle

- **WHEN** a requested directory is outside the root and when it does not exist
- **THEN** Kestrix SHALL answer both with the same status and message

### Requirement: Security response headers

Kestrix SHALL send a content security policy, `X-Content-Type-Options`,
`Referrer-Policy`, and a framing policy on every response it serves. The content
security policy SHALL be configurable so a deployment that serves the interface
from another origin can set its own, and the documentation interface MAY carry
its own policy without relaxing the application's.

#### Scenario: Application response carries the headers

- **WHEN** any route returns a response
- **THEN** the response SHALL carry a content security policy, `X-Content-Type-Options: nosniff`, a referrer policy, and a framing policy

#### Scenario: Policy is configurable

- **WHEN** a deployment configures its own content security policy
- **THEN** Kestrix SHALL send the configured policy instead of the default
