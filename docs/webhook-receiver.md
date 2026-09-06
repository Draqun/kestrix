# Webhook Receiver

The webhook receiver processes Git push events, classifies changed files, resolves the affected pipeline tasks, and optionally dispatches the resolved pipeline to GitLab CI.

## Overview

When a push event arrives at `POST /webhook`, the receiver:

1. Validates the request (optional `X-Gitlab-Token` check).
2. Parses the GitLab push event JSON into a normalized `PushEvent`.
3. Acquires the registered remote project at the push event's immutable
   `checkout_sha` and loads the pipeline definition from that checkout.
4. Classifies changed files as "code" or "non-code" using configurable patterns.
5. Matches changed files to declared `ci_repo` blocks.
6. Resolves the DAG to determine which tasks are triggered.
7. Optionally dispatches the resolved pipeline to a GitLab project via the dispatch client.
8. Records the event in durable webhook history.
9. Returns a JSON response with the triggered tasks and generated YAML.

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

On each registered-project webhook request, the receiver refreshes the remote
through the source-session manager and requests the exact immutable event SHA.
All `.tf` files in the project's relative pipeline directory are parsed and
merged into one project (see [Pipeline Definition](pipeline-definition.md)). If
the event revision cannot be acquired, the request fails without resolving or
dispatching tasks from a different revision.

## Dispatch Integration

When dispatch is configured and tasks are triggered, the receiver creates a GitLab pipeline with:

- The generated `.gitlab-ci.yml` embedded as the pipeline configuration.
- Pipeline variables:
  - `KESTRIX_TRIGGERED_TASKS` — comma-separated list of triggered tasks.
  - `KESTRIX_CHANGED_REPOS` — comma-separated list of changed repos.
  - `KESTRIX_DISPATCH_TIME` — UTC timestamp of the dispatch.

## Webhook History

Every parsed event is recorded in PostgreSQL before the receiver responds,
including events that are ignored because no registered project matches,
resolved without triggering a task, or failed during processing. History
survives a server restart.

A request rejected before its payload is understood is not recorded: a bad
`X-Gitlab-Token`, an unreadable body, or a payload that is not a GitLab push
event. Recording those paths would let an unauthenticated caller write rows,
and because retention is bounded by row count, evict real history.

A recording failure never fails the webhook: GitLab retries on a non-2xx
response, so a history outage would otherwise turn into a delivery outage. The
failure is logged and the receiver returns its normal response.

### Recorded Status

| Status | Meaning |
|--------|---------|
| `triggered` | Tasks were resolved for the push |
| `skipped` | The event resolved but triggered no task |
| `ignored` | No registered project matched the event's GitLab project ID |
| `error` | Processing failed, including a failed pipeline dispatch; the message is stored with the event |

Raw webhook payload bodies are not stored, so commit messages and author
identities stay out of the history table.

### Retention

History is bounded by a row count, not by age: a busy day would still fill a
time-based window. Once the limit is exceeded, the oldest events are discarded
inside the same transaction that records the new one.

| Setting | Default | Description |
|---------|---------|-------------|
| `KESTRIX_WEBHOOK_HISTORY_LIMIT` | `1000` | Maximum retained events; must be a positive integer |
| `webhook.history_limit` (config file) | `1000` | Same setting in `~/.kestrix/config.yaml`; the environment variable wins |

### Query API

```text
GET /api/logs
```

Returns recorded events newest first, in the same paginated envelope as
`/api/runs` and `/api/projects`:

```json
{
  "items": [
    {
      "id": 42,
      "received_at": "2026-09-01T10:00:00Z",
      "project_name": "my-service",
      "repo": "org/my-service",
      "branch": "main",
      "revision": "0123456789abcdef",
      "commits": 2,
      "triggered_tasks": ["lint", "test"],
      "status": "triggered",
      "dispatched_pipeline_id": 12345,
      "dispatched_pipeline_url": "https://gitlab.com/org/my-service/-/pipelines/12345"
    }
  ],
  "pagination": { "total": 1, "limit": 25, "offset": 0, "next": null, "prev": null }
}
```

| Parameter | Description |
|-----------|-------------|
| `limit` | Page size; one of `10`, `25`, `50`, `100` (default `25`) |
| `offset` | Number of events to skip (default `0`) |
| `project` | Exact project name as recorded: the registered project's name, or the name in the parsed pipeline when the receiver runs against a single pipeline directory |
| `status` | One of `triggered`, `skipped`, `ignored`, `error` |
| `since` | RFC3339 timestamp; events received at or after it |
| `until` | RFC3339 timestamp; events received at or before it |

Filters combine conjunctively. An unparseable value is rejected with
`400 Bad Request` and no events are returned.

The **Webhook Logs** page in the UI reads this endpoint directly, with the same
filters and pagination.

## Health Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /api/health` | Returns `{"status": "ok", "service": "kestrix", "repos": 0, "webhooks": <count>, "pipelines": <count>}` |
| `GET /ping` | Chi `Heartbeat` middleware; returns the plain text `.`, not JSON |

`GET /api/health` returns `503 Service Unavailable` when a backing store query
fails. The body then carries `"status": "degraded"` and omits the counter whose
query failed, so a health check cannot stay green while its store is down.

## Further Reading

- `openspec/specs/webhook-receiver/spec.md` — full specification and test scenarios.
- `openspec/specs/webhook-log-history/spec.md` — webhook history specification.
- `internal/web/webhook.go` — webhook parser and classifier implementation.
- `internal/web/handler_webhook.go` — webhook handler.
- `internal/store/pgstore_webhook.go` — history storage and retention.
- `internal/web/e2e_test.go` — end-to-end tests.
