# Webhook Receiver

The webhook receiver processes Git push events, classifies changed files, resolves the affected pipeline tasks, and optionally dispatches the resolved pipeline to GitLab CI.

## Overview

When a push event arrives at `POST /webhook`, the receiver:

1. Validates the request (optional `X-Gitlab-Token` check).
2. Parses the GitLab push event JSON into a normalized `PushEvent`.
3. Loads the pipeline definition from the configured pipeline directory.
4. Classifies changed files as "code" or "non-code" using configurable patterns.
5. Matches changed files to declared `ci_repo` blocks.
6. Resolves the DAG to determine which tasks are triggered.
7. Optionally dispatches the resolved pipeline to a GitLab project via the dispatch client.
8. Returns a JSON response with the triggered tasks and generated YAML.

## HTTP Endpoint

```text
POST /webhook
```

### Authentication

If `signature.enabled` is `true` in the configuration, the receiver checks the `X-Gitlab-Token` header. Requests without a matching token are rejected with `401 Unauthorized`.

### Response Format

```json
{
  "project": "my-service",
  "repo": "org/my-service",
  "branch": "main",
  "status": "triggered",
  "tasks": ["lint", "test", "build"],
  "dispatch": {
    "dispatched": true,
    "pipeline_id": 12345,
    "pipeline_url": "https://gitlab.com/org/my-service/-/pipelines/12345"
  }
}
```

| Field | Description |
|-------|-------------|
| `status` | `"triggered"`, `"skipped"` (no code changes), or `"error"` |
| `project` | Name of the Kestrix project |
| `tasks` | List of triggered task names |
| `dispatch` | Dispatch result (only when dispatch is enabled) |

## File Classification

The `FileClassifier` determines whether a changed file should trigger CI tasks:

### Default Code Extensions

`.go`, `.py`, `.rs`, `.js`, `.ts`, `.java`, `.c`, `.cpp`, `.h`, `.tf`, `.yaml`, `.yml`, `.toml`, `.json`

### Default Exclude Patterns

- `docs/` — documentation directories
- `.md` — Markdown files
- `LICENSE` — license files
- `CHANGELOG` — changelogs
- `.gitignore` — git ignore files

If a push only touches excluded files (e.g., a README update), the receiver responds with `"status": "skipped"` and no tasks are triggered.

## Repo Matching

The classifier maps changed files to declared `ci_repo` blocks:

| Repo Kind | Matching Logic |
|-----------|---------------|
| `subpath` | Files under the repo's `path` prefix |
| `git` (default) | Any code change in the push matches the whole repo |

## Pipeline Loading

On each webhook request, the receiver loads the pipeline definition from the configured directory. All `.tf` files in the directory are parsed and merged into a single project (see [Pipeline Definition](pipeline-definition.md)).

## Dispatch Integration

When dispatch is configured and tasks are triggered, the receiver creates a GitLab pipeline with:

- The generated `.gitlab-ci.yml` embedded as the pipeline configuration.
- Pipeline variables:
  - `KESTRIX_TRIGGERED_TASKS` — comma-separated list of triggered tasks.
  - `KESTRIX_CHANGED_REPOS` — comma-separated list of changed repos.
  - `KESTRIX_DISPATCH_TIME` — UTC timestamp of the dispatch.

## Health Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Returns `{"status": "ok"}` |
| `GET /ping` | Returns `{"status": "ok"}` |

## Further Reading

- `openspec/specs/webhook-receiver/spec.md` — full specification and test scenarios.
- `internal/web/webhook.go` — webhook parser and classifier implementation.
- `internal/web/server.go` (around line 1490) — webhook handler.
- `internal/web/e2e_test.go` — end-to-end tests.
