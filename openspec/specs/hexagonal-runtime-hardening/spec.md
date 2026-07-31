# hexagonal-runtime-hardening Specification

## Purpose
TBD - created by archiving change hexagonal-runtime-hardening. Update Purpose after archive.
## Requirements
### Requirement: Composition root SHALL live in cmd
The application composition root SHALL be owned by `cmd/kestrix/main.go`. HTTP server code SHALL receive already-built dependencies and SHALL NOT rebuild or repair them at runtime.

#### Scenario: Server is constructed with explicit dependencies
- **WHEN** the application starts
- **THEN** `cmd/kestrix/main.go` SHALL construct the server dependencies explicitly
- **AND** the web layer SHALL use those dependencies directly

### Requirement: RunService SHALL fail fast on missing dependencies
`RunService` SHALL require the dependencies it needs to execute a run and SHALL fail early when they are missing.

#### Scenario: Missing pipeline/task execution dependency is rejected
- **WHEN** `RunService` is created or invoked without the required runner/dependency wiring
- **THEN** the service SHALL return an explicit error rather than building fallback wiring at runtime

### Requirement: Webhook logging SHALL remain wired
Webhook logging SHALL stay connected through server wiring so webhook events continue to be recorded.

#### Scenario: Webhook sink is present
- **WHEN** the webhook handler processes an event and a sink is configured
- **THEN** the event SHALL be appended to the sink

### Requirement: Webhook processing SHALL be nil-safe
Webhook log processing SHALL tolerate a missing sink without crashing the application.

#### Scenario: Webhook sink is absent
- **WHEN** the webhook handler or service is invoked without a webhook sink
- **THEN** the code SHALL avoid a nil-pointer failure
- **AND** the request SHALL continue through the normal HTTP response path

