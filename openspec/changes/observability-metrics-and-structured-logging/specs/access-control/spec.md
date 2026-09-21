## ADDED Requirements

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
