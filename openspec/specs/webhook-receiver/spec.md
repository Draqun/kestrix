# Spec: Webhook Receiver

## Purpose
The webhook receiver processes Git push events, extracts relevant data, and forwards it to the DAG engine for task resolution.

## Overview

The webhook receiver is an HTTP server that accepts push events from GitLab (and other git hosts), classifies the changed files, and passes the event to the DAG engine for evaluation.

## Requirements

### Requirement: HTTP Endpoint (implemented)

The server SHALL expose a `POST /webhook` endpoint that accepts JSON payloads from GitLab push webhooks. The server SHALL also expose a `GET /api/health` endpoint that reports service status and stored counts, and a `GET /ping` heartbeat, for monitoring.

Every delivery to `POST /webhook` SHALL be authenticated before any part of its payload is trusted, and verification SHALL fail closed. A delivery is accepted only if it verifies, using the configured method (`gitlab` header comparison or `hmac` body signature), against either the matched registered project's own webhook secret or the global webhook secret. A delivery that verifies against neither, including one for which no secret is configured at all, SHALL be rejected with `401`, SHALL NOT be recorded in webhook history, and SHALL NOT trigger any pipeline load, source acquisition, resolution, or dispatch. Secret comparison SHALL be constant time. The rejection response SHALL be identical regardless of whether the project is unknown, has no secret, or presented a wrong secret, and SHALL NOT disclose which secret was consulted.

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

#### Scenario: Oversized delivery

- **WHEN** a delivery's body exceeds the configured webhook body limit
- **THEN** the server SHALL respond `413`
- **AND** SHALL NOT record the event

#### Scenario: Verification precedes parsing

- **WHEN** a delivery fails verification and its body is also not a valid GitLab push event
- **THEN** the server SHALL respond `401`, not `400`

### Requirement: Push Event Parsing (implemented)

The server SHALL parse GitLab push event payloads, extracting:
- Repository path_with_namespace
- Repository URLs (HTTP and SSH clone URLs, and the web URL when present)
- GitLab project ID
- Branch name (normalized from refs/heads/* or refs/tags/*)
- Changed files (deduplicated across all commits)
- Commit count
- The immutable checkout SHA

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

### Requirement: File Classification (implemented)

The server SHALL classify changed files as "code" or "non-code" based on configurable patterns. Files matching exclude patterns (docs/, .md, LICENSE) SHALL be classified as non-code.

#### Scenario: Code change triggers build

```
WHEN a push modifies handler.go
THEN the file SHALL be classified as code
AND the corresponding ci_repo SHALL be marked as changed
```

#### Scenario: Docs change does not trigger build

```
WHEN a push modifies docs/README.md only
THEN the file SHALL be classified as non-code
AND no tasks SHALL be triggered
```

### Requirement: Pipeline Loading (implemented)

For a registered project the server SHALL load the pipeline definition from
the project's primary source on each webhook request. When the delivery came
from the primary source, the definition SHALL be loaded at the event's
immutable checkout SHA. When the delivery came from a secondary source, the
definition SHALL be loaded from the primary source at the project's configured
`repo_ref`, and the event SHA SHALL be recorded against the secondary source
rather than checked out as the pipeline. Multiple .tf files in the pipeline
directory SHALL be merged into a single project. When the server runs against
a single pipeline directory with no registered project, it SHALL load that
directory as before.

#### Scenario: Load pipeline from directory

```
WHEN the server receives a webhook
AND the pipeline directory contains repos.tf and tasks.tf
THEN both files SHALL be parsed and merged into one project
```

#### Scenario: Primary push loads the event revision

- **WHEN** a push to a project's primary repository arrives with checkout SHA `abc123`
- **THEN** the pipeline definition SHALL be loaded from the primary source at `abc123`

#### Scenario: Secondary push loads the configured primary ref

- **WHEN** a push to a repository that is a secondary source of project `checkout` arrives with checkout SHA `def456`
- **THEN** the pipeline definition SHALL be loaded from `checkout`'s primary source at its configured `repo_ref`
- **AND** the recorded event revision SHALL be `def456`
- **AND** the primary source SHALL NOT be resolved to `def456`

### Requirement: Response Format (implemented)

The webhook handler SHALL respond with JSON containing:
- status: "ok" or "skipped"
- project name
- repo and branch
- changed_repos map
- list of triggered tasks with details
- generated gitlab-ci.yml content

#### Scenario: Successful resolution

```
WHEN the webhook is processed successfully
THEN the response SHALL include the generated gitlab-ci.yml
AND the tasks list SHALL show each task's name, trigger status, and dependencies
```

### REQ-1: Documentation inclusion (pending)
The webhook receiver documentation SHALL be included in the project‑docs site and shall be kept up‑to‑date with any behaviour changes.

#### Scenario: Documentation included in UI build
- **WHEN** the UI build runs,
- **THEN** the webhook receiver documentation shall be present in `ui/dist/docs`.

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

- **WHEN** a request is rejected because its token does not match, its body cannot be read, or its payload is not a GitLab push event
- **THEN** no webhook event SHALL be recorded

#### Scenario: Recording failure does not fail the request

- **WHEN** the history store is unavailable while an event is processed
- **THEN** the receiver SHALL still return its normal response
- **AND** the failure SHALL be logged

### Requirement: Project resolution by source

The server SHALL resolve the projects a delivery belongs to by matching the
event's repository URLs against every project's sources. When no source
matches, it SHALL fall back to the GitLab project ID lookup. When neither
matches, it SHALL record the delivery as `ignored` and respond
`{"status": "ignored", "reason": "unknown project"}` with HTTP 200.

#### Scenario: Secondary source resolves the project

- **WHEN** a push arrives from a repository that no project has as `repo_url` but that project `checkout` declares as a remote `ci_repo`
- **THEN** the server SHALL resolve the delivery to `checkout`

#### Scenario: GitLab project ID still resolves

- **WHEN** a push arrives whose repository URLs match no source
- **AND** the payload's `project.id` equals a project's `gitlab_project_id`
- **THEN** the server SHALL resolve the delivery to that project

#### Scenario: Nothing matches

- **WHEN** a push arrives whose URLs match no source and whose `project.id` matches no project
- **AND** the delivery verifies against the global webhook secret
- **THEN** the server SHALL respond HTTP 200 with `{"status": "ignored", "reason": "unknown project"}`
- **AND** SHALL record one history event with status `ignored`

#### Scenario: Unverified delivery is rejected before resolution is reported

- **WHEN** a push arrives that verifies against neither a matched project's `webhook_secret` nor the global webhook secret
- **THEN** the server SHALL respond HTTP 401 `{"error": "unauthorized"}`
- **AND** SHALL NOT record a history event
- **AND** the response SHALL NOT reveal whether any project matched

### Requirement: Delivery fans out to every matching project

When a delivery matches more than one project, the server SHALL verify the
delivery against each matched project's `webhook_secret` (or the global
secret), skip a project that does not verify without recording it, and
resolve, record, and (when configured) dispatch each verified project
independently. A failure
in one project SHALL NOT prevent the others from being processed. The
response SHALL list each project's outcome.

#### Scenario: Shared repository triggers two projects

- **WHEN** a push arrives from a repository that is a source of both `checkout` and `billing`
- **THEN** the server SHALL evaluate each project's DAG against its own changed set
- **AND** SHALL record one history event per project
- **AND** the response SHALL contain one result per project

#### Scenario: One project fails to load

- **WHEN** a delivery matches `checkout` and `billing` and `billing`'s pipeline fails to parse
- **THEN** `checkout` SHALL still be resolved and recorded
- **AND** `billing` SHALL be recorded with status `error`
- **AND** the response SHALL report both outcomes

### Requirement: Changed set for a secondary push

When the delivery came from a secondary source, the server SHALL mark the
`ci_repo` matching that source as changed when the push contains at least one
code-classified file, and SHALL NOT apply path-prefix matching of the push's
files against the primary repository's subpath `ci_repo` blocks.

#### Scenario: Secondary push marks its ci_repo changed

- **WHEN** a push to secondary source `shared` touches `lib/util.go`
- **THEN** the changed set SHALL be exactly `["shared"]`
- **AND** a subpath `ci_repo "lib" { path = "lib" }` of the primary repository SHALL NOT be marked changed

#### Scenario: Secondary push with only excluded files

- **WHEN** a push to a secondary source touches only `README.md`
- **THEN** the changed set SHALL be empty
- **AND** the delivery SHALL be recorded with status `skipped`

### Requirement: History records the matched source

Each recorded webhook event SHALL carry the name of the source it matched
(the project name for the primary source, the `ci_repo` name otherwise), and
`GET /api/logs` SHALL return it as `source_name`.

#### Scenario: Secondary delivery is attributed

- **WHEN** a push from secondary source `shared` is recorded for project `checkout`
- **THEN** the history item SHALL have `project_name` `checkout` and `source_name` `shared`
