## MODIFIED Requirements

### Requirement: HTTP Endpoint (implemented)

The server SHALL expose a `POST /webhook` endpoint that accepts JSON payloads from GitLab and GitHub push webhooks. The server SHALL also expose a `GET /api/health` endpoint that reports service status and stored counts, and a `GET /ping` heartbeat, for monitoring.

Every delivery to `POST /webhook` SHALL be authenticated before any part of its payload is trusted, and verification SHALL fail closed. A delivery is accepted only if it verifies against either the matched registered project's own webhook secret or the global webhook secret. A delivery that verifies against neither, including one for which no secret is configured at all, SHALL be rejected with `401`, SHALL NOT be recorded in webhook history, and SHALL NOT trigger any pipeline load, source acquisition, resolution, or dispatch. Secret comparison SHALL be constant time. The rejection response SHALL be identical regardless of whether the project is unknown, has no secret, or presented a wrong secret, and SHALL NOT disclose which secret was consulted.

A delivery carrying an `X-GitHub-Event` header SHALL be verified by HMAC-SHA256 over the raw body (`X-Hub-Signature-256`), regardless of the configured method. Every other delivery SHALL be verified with the configured method (`gitlab` header comparison or `hmac` body signature).

The verification method SHALL be configurable by the `--method` command-line flag or, when the flag is unset, by the `KESTRIX_WEBHOOK_METHOD` environment variable — the same flag/environment-variable equivalence the webhook secret itself already has. When neither sets a method, the server SHALL default to `gitlab`.

The request body SHALL be read at most once and SHALL be bounded by the configured webhook body limit; a body exceeding it SHALL be rejected with `413` and SHALL NOT be recorded.

#### Scenario: Health check

- **WHEN** a client sends `GET /api/health` and every backing store answers
- **THEN** the server SHALL respond with `200 OK` and JSON carrying `status` `"ok"`, `service`, `repos`, the stored webhook event count and the configured project count

#### Scenario: Health check with a failing store

- **WHEN** a client sends `GET /api/health` and a backing store query fails
- **THEN** the server SHALL respond with `503 Service Unavailable`
- **AND** the body SHALL carry `status` `"degraded"` and SHALL omit the counter whose query failed

#### Scenario: Heartbeat

- **WHEN** a client sends `GET /ping`
- **THEN** the server SHALL respond with `200 OK` and the plain text `.`

#### Scenario: Delivery verified by the project's own secret

- **WHEN** a push event arrives for a registered project that has a webhook secret
- **AND** the delivery verifies against that secret
- **THEN** the server SHALL process the event as before

#### Scenario: Delivery verified by the global secret

- **WHEN** a push event arrives for a registered project that has no webhook secret of its own
- **AND** the global webhook secret is configured and the delivery verifies against it
- **THEN** the server SHALL process the event as before

#### Scenario: No secret configured anywhere

- **WHEN** a push event arrives and neither the matched project nor the global configuration has a webhook secret
- **THEN** the server SHALL respond `401`
- **AND** SHALL NOT record the event, load a pipeline, acquire a source, or dispatch

#### Scenario: Wrong secret

- **WHEN** a push event presents a token or signature that verifies against neither the project's secret nor the global secret
- **THEN** the server SHALL respond `401`
- **AND** SHALL NOT record the event

#### Scenario: Unknown project does not learn anything

- **WHEN** a push event names a GitLab project ID that is not registered and does not verify against the global secret
- **THEN** the server SHALL respond `401` with a body and status byte-identical to the wrong-secret rejection

#### Scenario: Unknown project with a valid global secret is still ignored

- **WHEN** a push event names an unregistered GitLab project ID and verifies against the global secret
- **THEN** the server SHALL respond `200` with `{"status": "ignored", "reason": "unknown project"}` and record the event with status `ignored`, as before

#### Scenario: HMAC method is honoured

- **WHEN** the verification method is `hmac`
- **AND** a delivery carries an `X-Hub-Signature-256` header whose HMAC-SHA256 over the raw body matches the applicable secret
- **THEN** the server SHALL accept the delivery
- **AND** a delivery carrying only a matching `X-Gitlab-Token` header SHALL be rejected with `401`

#### Scenario: Method configured by environment variable

- **WHEN** `KESTRIX_WEBHOOK_METHOD=hmac` is set and `--method` is not passed
- **THEN** the server SHALL verify deliveries using HMAC body-signature comparison
- **AND** a delivery presenting only a matching `X-Gitlab-Token` header SHALL be rejected with `401`

#### Scenario: Flag takes precedence over the environment variable

- **WHEN** `KESTRIX_WEBHOOK_METHOD=hmac` is set and `--method=gitlab` is also passed
- **THEN** the server SHALL verify deliveries using `gitlab` header comparison

#### Scenario: Oversized delivery

- **WHEN** a delivery's body exceeds the configured webhook body limit
- **THEN** the server SHALL respond `413`
- **AND** SHALL NOT record the event

#### Scenario: Verification precedes parsing

- **WHEN** a delivery fails verification and its body is also not a valid GitLab push event
- **THEN** the server SHALL respond `401`, not `400`

#### Scenario: GitHub delivery is verified by HMAC under the default method

- **WHEN** the configured method is `gitlab`
- **AND** a delivery carries `X-GitHub-Event: push` and an `X-Hub-Signature-256` header whose HMAC-SHA256 over the raw body matches the applicable secret
- **THEN** the server SHALL accept the delivery

#### Scenario: GitHub delivery cannot fall back to a token

- **WHEN** a delivery carries `X-GitHub-Event: push` and a matching `X-Gitlab-Token` header but no valid `X-Hub-Signature-256`
- **THEN** the server SHALL respond `401`

### Requirement: Push Event Parsing (implemented)

The server SHALL parse GitLab and GitHub push event payloads, extracting:
- Repository path_with_namespace
- Repository URLs (HTTP and SSH clone URLs, and the web URL when present)
- GitLab project ID (GitLab only)
- Branch name (normalized from refs/heads/* or refs/tags/*)
- Changed files (deduplicated across all commits)
- Commit count
- The immutable checkout SHA

For a GitHub delivery the server SHALL read `repository.full_name`, `repository.clone_url`, `repository.ssh_url`, `repository.html_url`, `repository.default_branch`, `ref`, `after`, `deleted`, and `commits[].added`, `modified` and `removed`. A GitHub `ping` event SHALL be answered `200` with `{"status": "pong"}` after verification and SHALL NOT be recorded. Any other GitHub event SHALL be answered `400`. A verified GitHub delivery whose body is not JSON because it was sent form-encoded SHALL be answered `400` with a message naming `application/json` as the required content type.

#### Scenario: Parse a push event

```
WHEN the server receives a valid GitLab push webhook payload
THEN it SHALL extract the repo, branch, and changed files
AND the branch SHALL be normalized (refs/heads/main → main)
```

#### Scenario: Repository URLs are extracted

- **WHEN** the payload's `project` object carries `git_http_url`, `git_ssh_url`, and `web_url`
- **THEN** the parsed event SHALL carry all three
- **AND** a missing URL field SHALL be omitted rather than fail parsing

#### Scenario: Reject non-push events

```
WHEN the server receives an event with object_kind != "push"
THEN it SHALL respond with 400 Bad Request
```

#### Scenario: Parse a GitHub push event

- **WHEN** the server receives a verified GitHub `push` delivery for `refs/heads/main` whose commits added `src/a.go` and modified `src/b.go`
- **THEN** the parsed event SHALL carry the branch `main`, the checkout SHA from `after`, and both files as changed
- **AND** its repository URLs SHALL be the payload's `clone_url`, `ssh_url` and `html_url`

#### Scenario: GitHub ping

- **WHEN** a verified delivery carries `X-GitHub-Event: ping`
- **THEN** the server SHALL respond `200` with `{"status": "pong"}`
- **AND** SHALL NOT record a webhook event

#### Scenario: Unsupported GitHub event

- **WHEN** a verified delivery carries `X-GitHub-Event: pull_request`
- **THEN** the server SHALL respond with `400 Bad Request`

#### Scenario: Form-encoded GitHub delivery

- **WHEN** a verified GitHub delivery has content type `application/x-www-form-urlencoded`
- **THEN** the server SHALL respond `400` with a message naming `application/json`

### Requirement: Event Recording

The webhook receiver SHALL record every event it parses to durable webhook history before responding, including events that are ignored because no registered project matches, resolved without triggering tasks, or failed during processing. A request rejected before its payload is parsed SHALL NOT be recorded, so that an unauthenticated caller cannot evict history through the retention bound. A recording failure SHALL NOT change the receiver's response to the caller.

#### Scenario: Ignored event still recorded

- **WHEN** the receiver rejects an event because no registered project matches its GitLab project ID
- **THEN** the event SHALL be recorded with status `ignored`
- **AND** the caller SHALL still receive the ignored response

#### Scenario: Resolved event with no triggered tasks

- **WHEN** the receiver resolves an event and no task is triggered
- **THEN** the event SHALL be recorded with status `skipped`

#### Scenario: Unparsed request not recorded

- **WHEN** a request is rejected because its token or signature does not match, its body cannot be read, or its payload is not a push event
- **THEN** no webhook event SHALL be recorded

#### Scenario: Recording failure does not fail the request

- **WHEN** the history store is unavailable while an event is processed
- **THEN** the receiver SHALL still return its normal response
- **AND** the failure SHALL be logged
