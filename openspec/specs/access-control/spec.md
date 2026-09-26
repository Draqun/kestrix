# access-control Specification

## Purpose
Decides what an authenticated caller may do: three global roles, per-project
grants, and the enforcement that covers every HTTP route and the user interface
that reflects it.
## Requirements
### Requirement: Global roles

Kestrix SHALL assign every account exactly one global role from the ordered set
`viewer`, `operator`, `admin`, where each role includes the permissions of the
roles below it. `viewer` SHALL permit read-only access, `operator` SHALL add
starting, cancelling, and approving runs and editing pipeline definitions, and
`admin` SHALL add account, credential, and project lifecycle administration.

#### Scenario: Viewer attempts a run

- **WHEN** an account whose global role is `viewer` requests a run to start
- **THEN** Kestrix SHALL reject the request with `403`
- **AND** SHALL NOT create a run

#### Scenario: Operator is refused credential administration

- **WHEN** an account whose global role is `operator` creates, rotates, or revokes a credential
- **THEN** Kestrix SHALL reject the request with `403`

#### Scenario: Administrator inherits lower roles

- **WHEN** an account whose global role is `admin` performs a read or a run action
- **THEN** Kestrix SHALL permit it without a separate grant

### Requirement: Per-project role grants

Kestrix SHALL support granting an account a role on a named project. For a
request scoped to one project, the effective role SHALL be the higher of the
account's global role and its grant on that project. A grant SHALL NOT reduce
the permissions the global role already provides.

#### Scenario: Grant raises a viewer on one project

- **WHEN** a global `viewer` holds an `operator` grant on project `alpha` and starts a run on `alpha`
- **THEN** Kestrix SHALL permit the request

#### Scenario: Grant does not apply to other projects

- **WHEN** that same account starts a run on project `beta`, where it holds no grant
- **THEN** Kestrix SHALL reject the request with `403`

#### Scenario: Grant cannot demote

- **WHEN** a global `operator` holds a `viewer` grant on project `alpha` and starts a run on `alpha`
- **THEN** Kestrix SHALL permit the request

#### Scenario: Grants follow the project

- **WHEN** a project is deleted
- **THEN** its grants SHALL be removed with it

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

### Requirement: Webhook delivery stays outside user authentication

Kestrix SHALL continue to accept GitLab webhook deliveries authenticated by
their shared secret alone, without a user token, and SHALL NOT apply role checks
to them. The webhook route SHALL remain outside the authenticated API surface.

#### Scenario: GitLab delivers a push event

- **WHEN** GitLab posts a push event carrying a valid shared secret and no user token
- **THEN** Kestrix SHALL process the event as before

#### Scenario: Webhook secret is still enforced

- **WHEN** a webhook delivery presents a missing or wrong shared secret
- **THEN** Kestrix SHALL reject it as it does today
- **AND** a user token SHALL NOT substitute for the shared secret

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

### Requirement: Metrics endpoint authentication

The metrics endpoint SHALL sit outside `/api` and therefore outside the public
and protected `/api` route groups, and SHALL carry its own authentication. It
SHALL NOT be reachable without credentials at any time. Enabling metrics SHALL
NOT add a public route: the endpoint SHALL accept a dedicated scrape token
presented as a bearer token, or an authenticated account holding at least the
viewer role, and SHALL reject everything else with `401`. The scrape token SHALL
grant access to the metrics endpoint only and SHALL NOT authenticate any `/api`
route, carry a role, or create a session.

#### Scenario: Scrape token is not an API credential
- **WHEN** a request presents the scrape token as a bearer token to any `/api` route
- **THEN** the request SHALL be rejected with `401`
- **AND** the token SHALL NOT be resolved to any account or role

#### Scenario: Viewer may read metrics
- **WHEN** a signed-in account with the viewer role requests the metrics endpoint while metrics are enabled
- **THEN** the request SHALL be served

#### Scenario: Public route set is unchanged
- **WHEN** metrics are enabled
- **THEN** the set of routes reachable without credentials SHALL remain the health, login, logout, and first-run setup endpoints

### Requirement: Runner-job endpoint authentication

The runner-job endpoints (polling for a job, appending trace, reporting
completion) SHALL sit outside `/api` and SHALL NOT be reachable without a
valid runner bearer token. A request bearing a user session, an `/api` bearer
token, or no credentials SHALL be rejected with `401`. A runner token SHALL
authenticate the runner-job endpoints only and SHALL NOT grant access to any
`/api` route, carry a role, or create a user session.

#### Scenario: User session does not authenticate a runner-job endpoint
- **WHEN** a signed-in user's session cookie is presented to a runner-job endpoint
- **THEN** the request SHALL be rejected with `401`

#### Scenario: Runner token does not authenticate `/api`
- **WHEN** a runner's bearer token is presented to any `/api` route
- **THEN** the request SHALL be rejected with `401`
- **AND** the token SHALL NOT be resolved to any account or role

### Requirement: Runner management requires admin role

`POST /api/runners` and `DELETE /api/runners/{id}` sit inside `/api`, gated
by role-based authorization like every other admin-only route. An account
without the administrator role SHALL NOT be able to register or revoke a
runner.

#### Scenario: Runner registration requires admin
- **WHEN** an account without the administrator role calls `POST /api/runners`
- **THEN** the request SHALL be rejected with `403`

#### Scenario: Runner revocation requires admin
- **WHEN** an account without the administrator role calls `DELETE /api/runners/{id}`
- **THEN** the request SHALL be rejected with `403`

