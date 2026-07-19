# Spec: Webhook Receiver

## Overview

The webhook receiver is an HTTP server that accepts push events from GitLab (and other git hosts), classifies the changed files, and passes the event to the DAG engine for evaluation.

## Requirements

### Requirement: HTTP Endpoint

The server SHALL expose a `POST /webhook` endpoint that accepts JSON payloads from GitLab push webhooks. The server SHALL also expose `GET /health` and `GET /ping` endpoints for monitoring.

#### Scenario: Health check

```
WHEN a client sends GET /health
THEN the server SHALL respond with 200 OK and JSON {"status": "ok"}
```

### Requirement: Push Event Parsing

The server SHALL parse GitLab push event payloads, extracting:
- Repository path_with_namespace
- Branch name (normalized from refs/heads/* or refs/tags/*)
- Changed files (deduplicated across all commits)
- Commit count

#### Scenario: Parse a push event

```
WHEN the server receives a valid GitLab push webhook payload
THEN it SHALL extract the repo, branch, and changed files
AND the branch SHALL be normalized (refs/heads/main → main)
```

#### Scenario: Reject non-push events

```
WHEN the server receives an event with object_kind != "push"
THEN it SHALL respond with 400 Bad Request
```

### Requirement: File Classification

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

### Requirement: Pipeline Loading

The server SHALL load pipeline definitions from a configured directory on each webhook request. Multiple .tf files in the directory SHALL be merged into a single project.

#### Scenario: Load pipeline from directory

```
WHEN the server receives a webhook
AND the pipeline directory contains repos.tf and tasks.tf
THEN both files SHALL be parsed and merged into one project
```

### Requirement: Response Format

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
