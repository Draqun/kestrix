## MODIFIED Requirements

### Requirement: Actor representation

An actor SHALL be represented as one opaque string of the form `<kind>:<id>`,
where `<kind>` is a lowercase label and `<id>` is the identifier meaningful for
that kind. An action performed through an authenticated API request SHALL be
attributed to `user:<account name>`, using the account name resolved by
authentication for that request and never a caller-supplied value. A run
started by a verified push SHALL be attributed to `webhook:<source name>`,
where the source name is the matched project or `ci_repo` name the webhook
history records for that delivery. Kestrix SHALL NOT produce any other kind;
`cli:` stays reserved for a run origin that does not yet create run records.

#### Scenario: Authenticated start is attributed to the account

- **WHEN** an account named `alice` starts a run through the API
- **THEN** the run's `started_by` SHALL be `user:alice`

#### Scenario: Caller-supplied actor is ignored

- **WHEN** a request to start, approve, or cancel a run carries an actor name in its body, a header, or a query parameter
- **THEN** Kestrix SHALL attribute the action to the authenticated account only

#### Scenario: Push-started run is attributed to its source

- **WHEN** a verified push to the configured ref of project `shop`'s primary repository starts a run
- **THEN** the run's `started_by` SHALL be `webhook:shop`
