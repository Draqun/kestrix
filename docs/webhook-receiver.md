# Webhook Receiver

The webhook receiver processes Git push events, classifies changed files, resolves the affected pipeline tasks, and automatically starts a pipeline run (or optionally dispatches to GitLab CI if configured).

## Overview

When a push event arrives at `POST /webhook`, the receiver:

1. Validates the request (optional `X-Gitlab-Token` check).
2. Parses the GitLab push event JSON into a normalized `PushEvent`, including
   the repository URLs the payload carries.
3. Resolves the registered project(s) the pushed repository belongs to (see
   [Project Resolution](#project-resolution)).
4. For each matched project, acquires its primary repository and loads the
   pipeline definition from that checkout (see [Pipeline Loading](#pipeline-loading)).
5. Classifies changed files as "code" or "non-code" using configurable patterns.
6. Matches changed files to declared `ci_repo` blocks.
7. Resolves the DAG to determine which tasks are triggered.
8. Starts a pipeline run when the push is to the configured ref and at least one
   task is triggered (see [Run Execution](#run-execution)), or optionally dispatches
   to GitLab CI if dispatch is configured.
9. Records one event per matched project in durable webhook history.
10. Returns a JSON response with one result per matched project.

## HTTP Endpoint

```text
POST /webhook
```

### Authentication

Every delivery is authenticated before any part of its payload is trusted,
and verification fails closed: there is no configuration in which `/webhook`
accepts an unverified delivery.

A delivery is accepted only if it verifies, using the configured method
(`KESTRIX_WEBHOOK_METHOD`: `gitlab` header comparison, or `hmac` body
signature), against either of two secrets, checked in this order:

1. The matched registered project's own `webhook_secret` (set via
   `PUT /api/projects/{name}`), when the project is registered and has one.
2. The global `KESTRIX_WEBHOOK_SECRET`, when set.

The project secret is tried first only so the common case short-circuits,
not for security - either match accepts the delivery. A delivery that
verifies against neither, including one for which no secret is configured at
all, is rejected with `401 {"error":"unauthorized"}` and is **not** recorded
in webhook history. The rejection is byte-identical whether the project is
unknown, has no secret, or presented a wrong secret: a distinct message would
let a caller enumerate which GitLab project IDs are registered. Secret
comparison is constant time.

Verification runs before the body is parsed as a GitLab push event, so a
delivery that fails verification answers `401`, never `400`, regardless of
whether its payload is well-formed.

The request body is read at most once and is bounded by
`KESTRIX_WEBHOOK_MAX_BODY_BYTES`; a body over that bound is rejected with
`413` and is not recorded either.

A per-project secret can be set from the browser as well as from the API: the
project detail page's **Webhook setup** panel shows the delivery URL and the
Git host steps, and the create/edit dialogs set the secret. See
[Frontend](fe.md) - Project webhook setup.

**BREAKING**: a deployment with no `KESTRIX_WEBHOOK_SECRET` and no
per-project secret rejects every delivery with `401`. See
[Authentication](authentication.md) for how the global secret and timeouts
are configured, and the CHANGELOG for the migration note.

### Response Format

```json
{
  "status": "ok",
  "results": [
    {
      "project": "my-service",
      "source_name": "shared-lib",
      "status": "ok",
      "changed": ["shared-lib"],
      "tasks": ["lint", "test", "build"],
      "count": 3,
      "run_id": "018f2d5e-1234-7000-8000-000000000001",
      "dispatched": false
    }
  ],
  "project": "my-service",
  "changed": ["shared-lib"],
  "tasks": ["lint", "test", "build"],
  "count": 3,
  "run_id": "018f2d5e-1234-7000-8000-000000000001",
  "dispatched": false
}
```

| Field | Description |
|-------|-------------|
| `status` | `"ok"` when at least one project resolved, `"error"` when every matched project failed, `"ignored"` when no project matched |
| `results` | One entry per matched project, in project-name order |
| `results[].source_name` | The source the delivery matched: the project name for its primary repository, the `ci_repo` name for a secondary one |
| `results[].status` | `"ok"` when the project's pipeline was loaded and its DAG resolved, `"error"` otherwise (with `error` carrying the message). A failed dispatch keeps `"ok"` with `dispatched: false` and `error` set, and is recorded as `error` in history |
| `results[].run_id` | ID of the started pipeline run when triggered on configured ref without dispatch; omitted otherwise |
| `project`, `changed`, `tasks`, `count`, `run_id`, `dispatched`, `pipeline_id`, `pipeline_url` | Mirrored at the top level only when exactly one project matched, for callers written before fan-out |

When every matched project fails, the response status is `422` (`400` when
the push carries no immutable SHA) and the single-match document carries the
`error` message at the top level.

## Project Resolution

A registered project listens to a set of **sources**: its `repo_url` (the
primary source) and every `ci_repo` block in its pipeline that declares a
`url`. Sources are derived automatically when the project is created or
updated and whenever its pipeline definition is loaded from Git; they are
listed by `GET /api/projects/{name}` and cannot be edited by hand. See
[Remote projects](remote-projects.md#sources).

The receiver matches the payload's repository URLs (`git_http_url`,
`git_ssh_url`, `web_url`) against every project's sources. The comparison is
transport-neutral: `https://host/group/repo.git`, `ssh://git@host/group/repo`
and `git@host:group/repo.git` all identify the same repository. A URL that
embeds credentials is never matched, stored, or logged.

When no source matches, the receiver falls back to the payload's `project.id`
against `gitlab_project_id`, so registrations that predate sources keep
working. When neither matches, the delivery is recorded as `ignored` and the
response is `{"status": "ignored", "reason": "unknown project"}`.

Resolution happens before verification, on fields probed from the raw body,
so each matched project is verified against **its own** `webhook_secret` (or
the global secret) as described under Authentication. A matched project whose
secret does not verify is skipped without a history row while the others are
processed; a delivery that no matched project accepts answers `401`. When no
project matches at all, only the global secret can accept the delivery, and it
is then recorded as `ignored`.

### Fan-out

One repository may be a source of several projects (a shared library declared
by two services, for example). Such a delivery is processed once per project:
each project's DAG is evaluated against its own changed set, each gets its own
history row and its own dispatch, and the response lists every outcome in
`results`. A failure in one project never prevents the others from being
processed. For a secondary repository to reach Kestrix at all, add a webhook
on that repository in the Git host pointing at the same `/webhook` URL.

### Secondary pushes

A push to a secondary source cannot be checked out as the pipeline: the
pipeline is the primary repository's file. The receiver therefore loads the
definition from the primary source at the project's configured `repo_ref`, and
marks the matching `ci_repo` (found by its `url` in the just-loaded pipeline,
so a renamed block is still recognised) as changed **as a whole** when the
push contains at least one code-classified file. The pushed files are not path-matched
against the primary repository's subpath `ci_repo` blocks, so a directory name
shared by both repositories cannot produce a false trigger. The event SHA is
recorded in history and passed to the dispatched job as
`KESTRIX_SOURCE_REVISION`.

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

On each registered-project webhook request, the receiver refreshes the primary
remote through the source-session manager. For a push to the primary source it
requests the exact immutable event SHA; for a push to a secondary source it
requests the configured `repo_ref` (see [Secondary pushes](#secondary-pushes)).
All `.tf` files in the project's relative pipeline directory are parsed and
merged into one project (see [Pipeline Definition](pipeline-definition.md)). If
the revision cannot be acquired, that project's entry fails without resolving
or dispatching tasks from a different revision. A secondary push also refreshes the
project's derived sources, because it loads the configured ref. A primary push
never rewrites them: it loads its own immutable SHA, which is normally the
ref's head but need not be, since deliveries can arrive out of order or be
retried - deriving routing from an older commit would restore a `ci_repo` that
a later commit removed. A `ci_repo` added on the primary repository therefore
reaches routing at the next load of the configured ref: opening the project, or
`GET /api/projects/{name}/definition`.

### Branch Deletions

Push events that delete a Git branch (indicated by `after: "0000000000000000000000000000000000000000"`)
are detected immediately before any pipeline files are fetched or loaded. The delivery
is ignored for all matched projects, recorded in webhook history with status `ignored`
and reason `branch deleted`, and returns `{"status": "ignored", "reason": "branch deleted"}`.

## Run Execution

When a push event resolves triggered tasks and GitLab CI dispatch is not configured,
the webhook receiver automatically starts a pipeline run in Kestrix:

- **Ref matching**:
  - For a push to the **primary repository**, the event ref must match the project's
    configured `repo_ref` (e.g. `refs/heads/main`). Only branch pushes trigger runs;
    tags (even if identically named) do not match.
  - For a push to a **secondary repository**, the event ref must match the `ci_repo`
    declaration's `ref` attribute. If `ref` is omitted on the `ci_repo`, it falls
    back to the project's `default_branch` (or `main`).
- **Trigger condition**: At least one task must be resolved by the DAG (`count > 0`).
  A push that touches only ignored files (such as documentation) or produces 0 triggered
  tasks does not start a run.
- **Asynchronous start**: The run starts asynchronously via the run service in the
  background (respecting concurrency limits and queueing). The source session workspace
  cleanup is handed off to the run lifecycle so the run has exclusive access to the
  checkout until completion.
- **Attribution**: The run is recorded with `started_by = "webhook:<source_name>"`
  (for example, `webhook:my-service` for primary repo pushes, or `webhook:shared-lib`
  for secondary repo pushes).
- **Response**: The resulting run ID is returned in `results[].run_id` (and mirrored at
  the top level when exactly one project matched).

When GitLab dispatch is configured (`dispatch.project_id` / `gitlab_token`), dispatch
takes precedence and no local run is started.

## Dispatch Integration

When dispatch is configured and tasks are triggered, the receiver creates a GitLab pipeline with:

- The generated `.gitlab-ci.yml` embedded as the pipeline configuration.
- Pipeline variables:
  - `KESTRIX_TRIGGERED_TASKS` — comma-separated list of triggered tasks.
  - `KESTRIX_CHANGED_REPOS` — comma-separated list of changed repos.
  - `KESTRIX_DISPATCH_TIME` — UTC timestamp of the dispatch.
  - `KESTRIX_SOURCE_NAME` — the source that fired: the project name for a
    primary push, the `ci_repo` name for a secondary one.
  - `KESTRIX_SOURCE_REVISION` — the pushed commit SHA of that source, so a job
    can pin a secondary checkout to the revision that triggered it.

Dispatch is always bound to the matched project's `gitlab_project_id`; a
secondary repository cannot redirect it.

## Webhook History

Every parsed event is recorded in PostgreSQL before the receiver responds,
including events that are ignored because no registered project matches,
resolved without triggering a task, or failed during processing. History
survives a server restart. A delivery that fans out to several projects
records one row per project, each carrying `source_name`, the source it
matched; a busy shared repository therefore consumes retention faster.

A request rejected before its payload is understood is not recorded: a
failed verification (see Authentication above), an oversized body, or a
payload that is not a GitLab push event. Recording those paths would let an
unauthenticated caller write rows, and because retention is bounded by row
count, evict real history.

A recording failure never fails the webhook: GitLab retries on a non-2xx
response, so a history outage would otherwise turn into a delivery outage. The
failure is logged and the receiver returns its normal response.

### Recorded Status

| Status | Meaning |
|--------|---------|
| `triggered` | Tasks were resolved for the push |
| `skipped` | The event resolved but triggered no task |
| `ignored` | No registered project matched the event's repository URLs or GitLab project ID |
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
      "source_name": "my-service",
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

`source_name` is the project name when the push came from the project's own
repository and the `ci_repo` name when it came from a secondary source; the
Webhook Logs page shows it under the project when the two differ.

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
