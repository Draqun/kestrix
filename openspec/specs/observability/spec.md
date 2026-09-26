# observability Specification

## Purpose
Defines what Kestrix exposes about its own runtime to an operator: the metrics
an external scraper may read, the rules that govern access to them, and the
structured shape of the process log stream.
## Requirements
### Requirement: Metrics endpoint

Kestrix SHALL expose a `GET /metrics` endpoint in Prometheus text exposition
format version 0.0.4. The endpoint SHALL be disabled by default and SHALL exist
only when metrics are enabled by configuration. When disabled, a request to
`/metrics` SHALL be answered `404` and SHALL be indistinguishable from any other
unknown path.

#### Scenario: Metrics disabled by default
- **WHEN** Kestrix starts with no metrics setting
- **THEN** `GET /metrics` SHALL return `404`
- **AND** no metric SHALL be collected or retained in memory

#### Scenario: Metrics enabled
- **WHEN** metrics are enabled and an authorized scraper requests `GET /metrics`
- **THEN** the response SHALL be `200` with content type `text/plain; version=0.0.4; charset=utf-8`
- **AND** the body SHALL parse as valid Prometheus exposition text

### Requirement: Exposed metric set

When metrics are enabled, the endpoint SHALL expose at least: the total number
of runs that reached each terminal status (`passed`, `failed`, `canceled`), the
current number of `running` and `queued` runs, a histogram of completed run
durations in seconds, the total number of webhook deliveries by outcome
(accepted, rejected, skipped), and a build-information gauge whose value is `1`
and whose labels carry the same version string reported by `GET /api/version`.
Every metric SHALL carry a `HELP` and a `TYPE` line. Counters SHALL be
monotonic for the life of the process and SHALL restart at zero after a restart.

#### Scenario: Run outcome counted once
- **WHEN** a run reaches a terminal status
- **THEN** the counter for that status SHALL increase by exactly one
- **AND** the run's duration SHALL be observed once in the duration histogram

#### Scenario: Queue depth reflects the scheduler
- **WHEN** runs are queued because every concurrency slot is busy
- **THEN** the queued gauge SHALL equal the number of runs in `queued` state
- **AND** it SHALL return to zero once the queue drains

#### Scenario: Label cardinality is bounded
- **WHEN** metrics carry a project label
- **THEN** the number of distinct project label values SHALL be bounded by a documented cap
- **AND** projects beyond the cap SHALL be aggregated under a single overflow label value rather than dropped

### Requirement: Metrics endpoint is never public

The metrics endpoint SHALL require credentials whenever it is enabled. It SHALL
accept either a dedicated scrape token presented as a bearer token, or the
credentials of a signed-in account with at least the viewer role. A request with
no credentials, a malformed authorization header, or a wrong token SHALL be
rejected with `401` before any metric is rendered. When metrics are enabled and
no scrape token is configured, only signed-in accounts SHALL be able to read the
endpoint.

#### Scenario: Unauthenticated scrape rejected
- **WHEN** metrics are enabled and `GET /metrics` arrives without credentials
- **THEN** the response SHALL be `401`
- **AND** the response body SHALL NOT contain any metric value

#### Scenario: Scrape token accepted
- **WHEN** a scrape token is configured and a request presents it as a bearer token
- **THEN** the response SHALL be `200` with the metric text
- **AND** the request SHALL NOT require a user account or a session cookie

#### Scenario: Wrong token is not distinguished
- **WHEN** a request presents an incorrect scrape token
- **THEN** the response SHALL be `401` with the same body as a request with no credentials
- **AND** the comparison SHALL be constant-time

### Requirement: Structured process logging

Kestrix SHALL write its process log as one JSON object per line to standard
output. Every line SHALL carry a timestamp, a severity level, a message, and the
stable field names used for the entities it names (`run_id`, `project`,
`account`, `route`, `err`). No log line SHALL contain a secret value: the same
redaction applied to run output SHALL apply to log fields.

#### Scenario: Log line is machine readable
- **WHEN** Kestrix logs any event
- **THEN** the line SHALL be a single valid JSON object
- **AND** it SHALL carry a timestamp, a level, and a message field

#### Scenario: Secrets never reach the log
- **WHEN** an event names a value resolved from a credential or a webhook secret
- **THEN** the emitted field SHALL carry the redaction placeholder, not the value

### Requirement: Log level is configurable

Kestrix SHALL accept a log level of `debug`, `info`, `warn`, or `error`,
defaulting to `info`, and SHALL emit only records at or above that level.
An unrecognized level SHALL fail startup with an error naming the setting and
the accepted values.

#### Scenario: Level filters records
- **WHEN** the level is `warn`
- **THEN** `warn` and `error` records SHALL be written
- **AND** `info` and `debug` records SHALL NOT be written

#### Scenario: Unknown level fails startup
- **WHEN** the configured level is not one of the four accepted values
- **THEN** startup SHALL fail with an error naming the setting and the accepted values

