## ADDED Requirements

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
