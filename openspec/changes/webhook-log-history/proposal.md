## Why

Webhook history is the only place in Kestrix where the UI shows fabricated data. `internal/web/server.go` implements `domain.WebhookLogSink` as an unbounded in-memory slice, so every entry is lost on restart and grows without limit while the process lives. `ui/src/pages/WebhookLogs.jsx` compensates with a hard-coded `MOCK_LOGS` fallback and a hard-coded `http://localhost:8080/api/logs` URL, which also breaks in the Docker deployment profile (port `18080`). PostgreSQL is already the runtime store for projects, credentials, and runs; webhook events are the last piece of operational state that does not survive a restart.

## What Changes

- Persist every processed webhook event to PostgreSQL instead of an in-memory slice.
- Record the full event context: project, repository, branch, revision, commit count, triggered tasks, status, error, and dispatch result.
- Replace `GET /api/logs` with a paginated, filterable endpoint (by project, status, and date range) using the existing `internal/web/pagination` helpers, consistent with `GET /api/runs`.
- **BREAKING**: `GET /api/logs` returns the project-wide paginated envelope (`{items, pagination}`) already used by `/api/runs` and `/api/projects`, instead of a bare JSON array.
- Cap retention so the table cannot grow unbounded.
- Remove `MOCK_LOGS` and the hard-coded base URL from the UI; `WebhookLogs.jsx` renders live data with filters and pagination.
- Report the real persisted webhook count in `GET /api/health`, which currently reuses the in-memory slice length for both `pipelines` and `webhooks`.

## Capabilities

### New Capabilities
- `webhook-log-history`: durable, queryable webhook event history — persistence, retention, the query API, and the UI that reads it.

### Modified Capabilities
- `webhook-receiver`: every processed event, including rejected and ignored ones, SHALL be recorded durably rather than only returned in the response.

## Impact

- **Code**: new `internal/store/pgstore_webhook.go`; schema migration in `internal/store/pgstore.go`; `domain.WebhookLogSink` port gains typed entries and query methods; `internal/service/service.go` `WebhookService`; `internal/web/server.go` (drops `webhookLog`/`webhookMu`), `handler_system.go`, `handler_webhook.go`; `ui/src/pages/WebhookLogs.jsx`.
- **API**: `GET /api/logs` response shape and query parameters change; swagger annotations regenerated.
- **Database**: one new table plus indexes; additive migration, no data backfill.
- **Tests**: `pgstore_integration_test.go`, webhook handler tests, and a UI component test replacing the mock-data assertion.

## Non-goals

- Notifications on webhook failure (`notifications` follow-up).
- Authentication or per-project access control on the history endpoint (`rbac-auth` follow-up).
- Storing raw webhook payload bodies.
- Replaying or re-dispatching a recorded event.
- Log export or long-term archival to object storage.
