## Context

See `proposal.md` — Why.

Current state that shapes the approach:

- `domain.WebhookLogSink` (`internal/domain/ports.go:193`) is an untyped port: `Append(map[string]interface{})` and `List() []map[string]interface{}`.
- `Server` implements that port over an unbounded slice guarded by `webhookMu` (`internal/web/server.go:201`). Nothing trims it.
- `handler_webhook.go` builds `logEntry` as a loose map and calls `s.logWebhook` at seven distinct exit points, so partial paths (unknown project, missing SHA, load failure) already record something.
- `handler_system.go:handleGetLogs` copies the slice and writes a bare array. `handleHealth` reports the same slice length as both `pipelines` and `webhooks`.
- PostgreSQL is mandatory at runtime; schema lives in one idempotent `migrate(db)` in `internal/store/pgstore.go:45`, using `CREATE TABLE IF NOT EXISTS` plus an ordered list of additive `ALTER TABLE ... IF NOT EXISTS` statements.
- `internal/web/pagination` already provides `Parse` and `Links`, and `/api/runs` and `/api/projects` already answer with `{items, pagination}`.
- `ui/src/api/client.js` centralizes `API_BASE` (`import.meta.env.VITE_API_URL || ''`) and already exports `getWebhookLogs()`. `WebhookLogs.jsx` bypasses both, calling `http://localhost:8080/api/logs` directly and falling back to `MOCK_LOGS`.

## Goals / Non-Goals

**Goals:**

- Move webhook history from process memory to PostgreSQL without changing the webhook receiver's HTTP contract to GitLab.
- Give the port a typed shape so the recorded fields are checked at compile time rather than by map-key convention.
- Keep the history endpoint consistent with the existing paginated list endpoints.
- Bound storage growth so history never becomes an operational problem.

**Non-Goals:**

- A generic audit-log subsystem. This stores webhook events only.
- A migration path for existing in-memory entries: they are per-process and already lost on restart, so there is nothing to back-fill.
- Changing how the receiver resolves or dispatches pipelines.

## Architecture

```
  GitLab push
      |
      v
  +---------------------------+
  | handler_webhook.go        |
  |  parse -> resolve         |
  |  -> dispatch              |
  +------------+--------------+
               | domain.WebhookEvent (typed)
               v
  +---------------------------+
  | service.WebhookService    |
  |  Record(event)            |  errors logged, never propagated
  +------------+--------------+
               | domain.WebhookLogStore
               v
  +---------------------------+      +---------------------------+
  | store.PGStore             |----->| webhook_events table      |
  |  RecordWebhookEvent       |      |  + trim to retention limit|
  |  ListWebhookEvents        |      |  idx (received_at DESC)   |
  |  CountWebhookEvents       |      |  idx (project_name)       |
  +---------------------------+      +---------------------------+
               ^
               | GET /api/logs?limit&offset&project&status&since&until
  +------------+--------------+
  | handler_system.go         |
  |  pagination.Parse/Links   |
  |  -> {items, pagination}   |
  +------------+--------------+
               ^
  +------------+--------------+
  | ui/src/api/client.js      |
  |  getWebhookLogs(filters)  |  API_BASE, no hard-coded host
  +------------+--------------+
               ^
  | WebhookLogs.jsx: filters + pagination, no MOCK_LOGS
```

## Decisions

### D1: A dedicated `webhook_events` table, not a column on `runs`

A webhook event is not a run. Most events never produce a run: they are ignored (unknown project), skipped (nothing triggered), or errored before resolution. Attaching them to `runs` would force nullable project references and pollute run statistics that `CountRunsByStatus` already feeds to the dashboard.

*Alternative rejected:* reuse `runs` with a `kind` discriminator — cheaper migration, but every existing run query would need a filter, and the dashboard counts would silently change meaning.

Columns follow the `runs` precedent: scalars as typed columns, the variable-shaped parts (`triggered_tasks`, `dispatch`) as `JSONB`. `project_name` is nullable and, unlike `runs.project_name`, carries **no** foreign key to `projects` — history must outlive the deletion of a project, and events for unregistered projects have no row to reference.

### D2: Replace the untyped port with `domain.WebhookEvent`

`map[string]interface{}` spreads key names across the handler, the mock, and the UI, with nothing to catch a typo. Introduce:

```go
type WebhookEvent struct {
    ID             int
    ReceivedAt     time.Time
    ProjectName    string   // empty when unmatched
    Repo           string
    Branch         string
    Revision       string
    Commits        int
    TriggeredTasks []string
    Status         string   // triggered | skipped | ignored | error
    Error          string
    DispatchedID   int
    DispatchedURL  string
}
```

and split the port in two: `WebhookLogSink { Record(WebhookEvent) }` for the write path, `WebhookLogStore { List(WebhookEventFilter, limit, offset) ([]WebhookEvent, error); Count(WebhookEventFilter) (int, error) }` for the read path. Two narrow interfaces keep the handler that writes from depending on the query surface, matching the existing `ProjectStore`/`RunStore` split.

*Trade-off:* touches `internal/mocks/mocks.go` and every test that asserts on log-entry maps. Worth it once, since the field set is now a stored schema rather than an ad-hoc map.

### D3: Count-based retention, enforced on write

After each insert, delete rows outside the newest `N` (`KESTRIX_WEBHOOK_HISTORY_LIMIT`, default `1000`), in the same transaction as the insert:

```sql
SELECT pg_advisory_xact_lock(<key>);
DELETE FROM webhook_events
WHERE id NOT IN (SELECT id FROM webhook_events ORDER BY received_at DESC, id DESC LIMIT $1)
```

The transaction-scoped advisory lock is required, not incidental. Under READ COMMITTED each transaction evaluates "newest N" against its own snapshot, so concurrent writers do not see each other's insert and each one leaves an extra row behind — history then settles at `N + concurrent writers`, breaking the retention requirement. The lock serializes only the insert-and-trim pair; it is released on commit or rollback and does not block reads.

*Alternative rejected:* time-based TTL (`received_at < now() - interval`). Age does not bound rows — a busy day still fills the table. A row count is the property that actually needs bounding.

*Alternative rejected:* a background sweeper goroutine. More moving parts and another lifecycle to test, for a table that only grows on webhook arrival.

### D4: Recording never fails the webhook

`WebhookService.Record` logs and swallows store errors. GitLab retries webhooks on non-2xx responses; failing a push because history is unavailable would turn a reporting outage into a delivery outage. This is stated normatively in the `webhook-receiver` delta.

### D5: The endpoint keeps its path, changes its shape

`GET /api/logs` stays, but returns `{items, pagination}`. The only consumers are `WebhookLogs.jsx` and the swagger docs, both updated here. Keeping the path avoids a second route to deprecate; the shape change is called out as BREAKING in the proposal because the response is externally observable.

Filters map to SQL predicates combined with `AND`: `project` (exact `project_name`), `status` (exact), `since`/`until` (RFC3339, inclusive on `received_at`). Unparseable filter values are a 400, consistent with `pagination.Parse`.

### D6: `handleHealth` reports a real count

`webhooks` becomes `CountWebhookEvents` with an empty filter. `pipelines` currently mirrors the same slice length, which was already wrong; it becomes the registered project count so the two fields stop being the same number.

## Security Considerations

- **Stored data**: events record repository paths, branch names, commit SHAs, and task names. No credentials pass through this path. Raw webhook payloads are explicitly not stored (proposal — Non-goals), which keeps commit messages and author identities out of the table.
- **Error text**: `logEntry["error"]` currently receives raw error strings from pipeline loading and source acquisition. Those can embed remote URLs. Record them as-is (they already reach the HTTP response today) but never include credential material — source acquisition already redacts credentials before wrapping errors, and a test asserts a credential-bearing clone failure is stored redacted.
- **Exposure**: `GET /api/logs` remains unauthenticated, exactly as today. This change does not widen exposure — the same data is already served — but it does make it durable, so a reader now sees history rather than only the current process's events. Authentication is the `rbac-auth` follow-up and is called out in the proposal's Non-goals.
- **Injection**: all queries are parameterized. The retention `DELETE` uses a bound `LIMIT`; the filter builder appends placeholders, never interpolated values.

## Risks / Trade-offs

- **A write per webhook adds DB load on the hot path** → The insert plus trim is one transaction on a table bounded at ~1000 rows; the receiver already does far heavier work (clone, parse, dispatch) on the same request.
- **The trim `DELETE` runs on every insert, behind an advisory lock** → Webhook writes serialize on one lock. The work inside it is bounded by the retention limit and indexed on `(received_at DESC, id DESC)`, and the receiver already does far heavier work (clone, parse, dispatch) outside the transaction. If it ever shows up in profiles, the trim can move behind an "every Nth insert" counter without changing the port.
- **Port change ripples through tests** → `internal/mocks/mocks.go`, `webhook_test.go`, `unit_gaps_test.go`, and `webhook_log_concurrent_test.go` all touch the map shape. Update them in the same task that changes the port, so the tree never sits broken between tasks.
- **Response shape change breaks any unknown API consumer** → Only the bundled UI and swagger are known consumers; the API carries no compatibility guarantee at this stage. Documented as BREAKING.
- **`webhook_log_concurrent_test.go` asserts on in-memory concurrency** → Its subject disappears. Replace it with a store-level concurrency test rather than deleting the coverage.

## Migration Plan

1. Schema migration is additive: `CREATE TABLE IF NOT EXISTS webhook_events` plus indexes, appended to the existing `migrate(db)` statement list. No backfill — in-memory entries are per-process and already ephemeral.
2. Deploy is a normal rolling restart. On start, history is empty and fills from the next webhook.
3. **Rollback**: revert the binary. The table is left in place; an older binary ignores it and falls back to in-memory logging. No down-migration is needed, and no data other than webhook history is affected.

## Open Questions

- Default retention of 1000 events is a guess at what an operator wants to scroll through. It is a configuration value, so it can be re-tuned after the first deployment without touching specs or tasks.
