## 1. Domain port and typed event

- [x] 1.1 Add `domain.WebhookEvent` and `domain.WebhookEventFilter` to `internal/domain/ports.go` per design D2, and verify `go build ./...` succeeds.
- [x] 1.2 Replace `domain.WebhookLogSink` with `Record(WebhookEvent)` and add `domain.WebhookLogStore` (`List`, `Count`); verify with `go vet ./internal/domain/...`.
- [x] 1.3 Update `internal/mocks/mocks.go` `MockWebhookLogSink` to the typed port plus an in-memory `WebhookLogStore` mock, and verify `go build ./...` compiles all test packages via `go vet ./...`.

## 2. PostgreSQL storage

- [x] 2.1 Add the `webhook_events` table and its `(received_at DESC, id DESC)` and `project_name` indexes to `migrate()` in `internal/store/pgstore.go`; verify a fresh `make test-integration` run creates the table and a second run is a no-op.
- [x] 2.2 Create `internal/store/pgstore_webhook.go` with `RecordWebhookEvent`, `ListWebhookEvents`, and `CountWebhookEvents`, using parameterized filter predicates per design D5; verify with new cases in `pgstore_integration_test.go` covering each filter and their conjunction.
- [x] 2.3 Enforce count-based retention inside the insert transaction per design D3, reading the limit from configuration; verify with an integration test that writes limit+5 events and asserts the oldest are gone and the newest remain.
- [x] 2.4 Add `KESTRIX_WEBHOOK_HISTORY_LIMIT` (default 1000) to `internal/config`, `.env.template`, and both Compose profiles; verify with a config unit test for default, override, and invalid value.

## 3. Recording path

- [x] 3.1 Change `service.WebhookService` to accept and record `domain.WebhookEvent`, logging and swallowing store errors per design D4; verify with a `webhook_service_test.go` case asserting a failing sink does not return an error.
- [x] 3.2 Rewrite `logEntry` in `internal/web/handler_webhook.go` as a typed `domain.WebhookEvent` across all seven exit paths; verify existing `webhook_test.go` cases pass after updating their assertions.
- [x] 3.3 Remove `webhookLog`/`webhookMu` and the `Append`/`List` methods from `internal/web/server.go`, wiring the PG store in their place; verify `grep -rn "webhookLog" internal/` returns nothing and `make test` passes.
- [x] 3.4 Replace `internal/web/webhook_log_concurrent_test.go` with a store-level concurrent-write test per the design's risk note; verify it passes under `go test -race ./internal/store/...`.
- [x] 3.5 Add handler tests covering the `ignored`, `skipped`, and `error` recording scenarios from the `webhook-receiver` delta spec, and verify each asserts the recorded status.

## 4. Query API

- [x] 4.1 Rewrite `handleGetLogs` in `internal/web/handler_system.go` to use `pagination.Parse`/`pagination.Links` and return `{items, pagination}`; verify with a handler test asserting the envelope matches `/api/runs`.
- [x] 4.2 Add `project`, `status`, `since`, and `until` query parameters with 400 on unparseable values; verify with handler tests for each filter and for a malformed `since`.
- [x] 4.3 Point `handleHealth` at `CountWebhookEvents` and the project count per design D6; verify with a test asserting `webhooks` and `pipelines` are no longer the same value.
- [x] 4.4 Update the swagger annotations on both handlers and run `make swagger`; verify `docs/swagger.json` shows the new response shape and parameters.

## 5. User interface

- [x] 5.1 Implement `getWebhookLogs(filters)` in `ui/src/api/client.js` against `API_BASE` with the new envelope; verify with a `client.test.js` case asserting the request URL and parsed items.
- [x] 5.2 Delete `MOCK_LOGS` and the hard-coded `http://localhost:8080` from `ui/src/pages/WebhookLogs.jsx`, rendering live data through the client; verify `grep -n "MOCK_LOGS\|localhost:8080" ui/src/pages/WebhookLogs.jsx` returns nothing.
- [x] 5.3 Add explicit empty and error states per the `webhook-log-history` UI requirement; verify with a `WebhookLogs.component.test.jsx` covering empty response and failed request.
- [x] 5.4 Add project, status, and date-range filters plus pagination controls to the page; verify with a component test asserting a status selection issues the filtered request.

## 6. Documentation

- [x] 6.1 Document durable webhook history, the retention limit, and the `/api/logs` parameters in `docs/webhook-receiver.md`; verify the page renders in `make ui-build` output under `ui/dist/docs`.
- [x] 6.2 Record the BREAKING `/api/logs` shape change and the new configuration variable in `CHANGELOG.md` under Unreleased; verify the entry names both.

## 7. Verification

- [x] 7.1 Run `make quality` and `make test-integration`; verify both pass with no new lint findings.
- [x] 7.2 Run `make ui-test` and `make ui-lint`; verify both pass.
- [x] 7.3 Restart the stack and confirm history survives: send a webhook, restart, and verify `GET /api/logs` still returns the event.
- [x] 7.4 Run `openspec validate webhook-log-history --strict --no-interactive`; verify it reports no issues.
