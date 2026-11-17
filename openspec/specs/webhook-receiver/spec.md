# Spec: Webhook Receiver

## Purpose
The webhook receiver processes Git push events, extracts relevant data, and forwards it to the DAG engine for task resolution.

## Overview

The webhook receiver is an HTTP server that accepts push events from GitLab (and other git hosts), classifies the changed files, and passes the event to the DAG engine for evaluation.

## Requirements

### Requirement: HTTP Endpoint (implemented)

The server SHALL expose `POST /webhook` accepting JSON push webhooks from GitLab and GitHub, rejecting unverified deliveries with HTTP 401 using constant-time comparison against project or global secrets. GitHub payloads SHALL require valid `X-Hub-Signature-256` HMAC signatures, while GitLab uses the configured method (`gitlab` header or `hmac`). Bodies exceeding the configured limit SHALL return 413.

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

The server SHALL parse GitLab and GitHub push payloads into repository identifiers, clone URLs, branch names, deduplicated file changes, commit counts, and checkout SHAs. Verified GitHub `ping` events SHALL return HTTP 200 `pong`, while non-push events or non-JSON encodings SHALL return HTTP 400.

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

For registered projects, the server SHALL load pipeline definitions from the primary source: at the event SHA for primary deliveries, or at the configured `repo_ref` for secondary source deliveries while attributing the event SHA to the secondary source. Multiple `.tf` pipeline files SHALL merge into one project definition.

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

- **WHEN** a request is rejected because its token or signature does not match, its body cannot be read, or its payload is not a push event
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

### Requirement: A push to the configured ref starts a run

When a verified push matches a registered project, targets its configured ref (`repo_ref` or `ci_repo` ref), resolves triggered tasks, and does not dispatch to GitLab CI, Kestrix SHALL start a pipeline run under standard concurrency, secret, and redaction rules, returning the `run_id` in the webhook response.

#### Scenario: Push to the configured branch starts a run
- **WHEN** a verified push to `main` of a project whose `repo_ref` is `main` triggers the task `build`
- **THEN** a run of that project SHALL be started at the pushed SHA with `build` triggered
- **AND** that project's result in the response SHALL carry the run's `run_id`

#### Scenario: Push to another branch starts nothing
- **WHEN** a verified push to `feature/x` of a project whose `repo_ref` is `main` triggers tasks
- **THEN** the event SHALL be recorded as before
- **AND** no run SHALL be started

#### Scenario: A tag named like the branch starts nothing
- **WHEN** a verified push of the tag `main` arrives for a project whose `repo_ref` is `main`
- **THEN** no run SHALL be started

#### Scenario: Secondary push to its ref starts a run
- **WHEN** a verified push to `release` of a secondary repository declared as a `ci_repo` with `ref = "release"` marks it changed and triggers a task
- **THEN** a run of the project SHALL be started with that `ci_repo` in its changed set

#### Scenario: No triggered task starts nothing
- **WHEN** a verified push to the configured ref triggers no task
- **THEN** the event SHALL be recorded as `skipped`
- **AND** no run SHALL be started

#### Scenario: Dispatch replaces the run
- **WHEN** GitLab dispatch is enabled and a GitLab project ID resolves for the matched project
- **THEN** the pipeline SHALL be dispatched as before
- **AND** no Kestrix run SHALL be started

#### Scenario: Secret resolution failure is recorded on a failed run
- **WHEN** a push starts a run whose pipeline references a secret that cannot be resolved
- **THEN** a failed run naming the secret error SHALL be recorded, as for a run started from the interface

### Requirement: A branch deletion is ignored

A verified push that deletes a branch, whose checkout revision is the
all-zero SHA or which the payload marks as deleted, SHALL be recorded with
status `ignored` and the reason `branch deleted`. It SHALL NOT load a
pipeline, acquire a source, dispatch, or start a run.

#### Scenario: Deleting a branch
- **WHEN** a verified push for a registered project carries `after` `0000000000000000000000000000000000000000`
- **THEN** the event SHALL be recorded as `ignored` with reason `branch deleted`
- **AND** no pipeline SHALL be loaded and no run SHALL be started
