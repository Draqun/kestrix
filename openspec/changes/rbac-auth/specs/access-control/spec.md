## Purpose

Decides what an authenticated caller may do: three global roles, per-project
grants, and the enforcement that covers every HTTP route and the user interface
that reflects it.

## ADDED Requirements

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

Kestrix SHALL require a valid token on every `/api` route except the health and
login endpoints. A request with no credentials, a malformed authorization
header, or an invalid token SHALL be rejected with `401` before the route's
handler runs. A route added later SHALL be covered by this requirement unless
it is deliberately registered as public.

#### Scenario: Call without credentials

- **WHEN** a request reaches any protected `/api` route with no authorization header
- **THEN** Kestrix SHALL reject it with `401`
- **AND** the handler SHALL NOT run

#### Scenario: Malformed authorization header

- **WHEN** a request presents an authorization header that is not a bearer token
- **THEN** Kestrix SHALL reject it with `401`

#### Scenario: Health and login stay reachable

- **WHEN** a request reaches the health or login endpoint without credentials
- **THEN** Kestrix SHALL serve it

### Requirement: Authorization on every route

Kestrix SHALL map every protected route to a minimum required role and SHALL
reject a request whose effective role is below it with `403`, distinct from the
`401` returned for a failed authentication. Read routes SHALL require `viewer`,
run and pipeline-editing routes SHALL require `operator`, and account,
credential, and project lifecycle routes SHALL require `admin`. The role SHALL
be taken from the verified token, never from a request header, query parameter,
or body field.

#### Scenario: Insufficient role is distinguishable from missing credentials

- **WHEN** an authenticated account calls a route above its effective role
- **THEN** Kestrix SHALL respond `403`
- **AND** an unauthenticated call to the same route SHALL respond `401`

#### Scenario: Caller-supplied role is ignored

- **WHEN** a request carries a role in a header, query parameter, or body alongside a valid token
- **THEN** Kestrix SHALL evaluate permission using only the role in the token

#### Scenario: Manual approval requires operator

- **WHEN** a global `viewer` approves a manual task
- **THEN** Kestrix SHALL reject the request with `403`

### Requirement: Account administration API

Kestrix SHALL expose administrator-only endpoints to list accounts, create an
account, change an account's global role, set an account's project grants, and
delete an account. Responses SHALL carry the account name, global role, grants,
and timestamps, and SHALL NOT carry password hashes.

#### Scenario: Non-administrator is refused

- **WHEN** an account whose global role is `viewer` or `operator` lists or modifies accounts
- **THEN** Kestrix SHALL reject the request with `403`

#### Scenario: Administrator changes a role

- **WHEN** an administrator changes another account's global role
- **THEN** subsequent requests from that account SHALL be evaluated against the new role once its next token is issued

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
attach the token to every API call, and SHALL return the user to the login view
when the API reports that authentication failed. The interface SHALL NOT offer
actions the current effective role cannot perform.

#### Scenario: Unauthenticated visitor

- **WHEN** a visitor with no valid token opens any application view
- **THEN** the interface SHALL show the login view
- **AND** SHALL NOT display project, run, or credential data

#### Scenario: Session ends while the interface is open

- **WHEN** an API call reports that authentication failed
- **THEN** the interface SHALL discard the stored token and return to the login view

#### Scenario: Viewer sees no mutating actions

- **WHEN** an account whose effective role is `viewer` views a project
- **THEN** the interface SHALL NOT offer run, edit, or delete actions for it

#### Scenario: Hidden actions are still refused by the API

- **WHEN** a request for a hidden action reaches the API anyway
- **THEN** Kestrix SHALL reject it with `403`
