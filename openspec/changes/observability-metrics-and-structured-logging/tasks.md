## 1. Configuration

- [ ] 1.1 Add `metrics.enabled` (`KESTRIX_METRICS_ENABLED`, default `false`), `metrics.token` (`KESTRIX_METRICS_TOKEN`, secret, no default) and `logging.level` (`KESTRIX_LOG_LEVEL`, default `info`) to `internal/config`, wired through the same reconciliation path as the run-scheduling settings; verify with unit tests covering file-over-environment, environment-fills-absent-key, and invalid value for each setting
- [ ] 1.2 Add the three settings to the startup source report with `metrics.token` reported by source only, never by value; verify with a unit test asserting the token value is absent from the rendered report
- [ ] 1.3 Fail startup with an error naming the setting when `logging.level` is outside `debug|info|warn|error` or `metrics.enabled` is not a boolean; verify with unit tests asserting the error text names the setting and no other value
- [ ] 1.4 Document the three settings in `.env.template`, the `README.md` settings table, and `docs/deployment.md`; verify by grepping each name in all three files

## 2. Structured logging

- [ ] 2.1 Install a `slog.JSONHandler` at the configured level in `cmd/kestrix` before the server and runner are constructed, and route the stdlib `log` package into it so `chiMiddleware.Logger` shares the stream; verify with a unit test capturing stdout and asserting one JSON object per line for both a slog call and a stdlib `log.Printf`
- [ ] 2.2 Rewrite the 20 `log.Printf` call sites in `internal/web` to `slog` with the stable field names (`run_id`, `project`, `account`, `route`, `err`); verify `grep -rn "log\.Printf" internal/web` returns nothing and the package's unit tests stay green
- [ ] 2.3 Rewrite the 7 `log.Printf` call sites in `internal/service` and the 1 in `internal/store` the same way; verify `grep -rn "log\.Printf" internal/service internal/store` returns nothing
- [ ] 2.4 Route every logged `err` field through the existing `internal/service` redactor; verify with a unit test that feeds a known credential value through a failing path and asserts the placeholder, not the value, in the emitted line
- [ ] 2.5 Assert level filtering end to end: at `warn`, `info` and `debug` records are absent and `warn`/`error` present; verify with a unit test per level

## 3. Metric registry

- [ ] 3.1 Create `internal/metrics` with a `Recorder` interface (`RunFinished`, `RunsInFlight`, `WebhookDelivery`), a real registry, and a no-op implementation; verify with unit tests over both implementations
- [ ] 3.2 Implement the text exposition renderer (counters, gauges, one duration histogram, build-info gauge) with `HELP` and `TYPE` lines and label-value escaping; verify against a golden-fixture test of the whole rendered page
- [ ] 3.3 Implement the project label cap (100 distinct values, overflow collapses to `__other__`); verify with a unit test registering 101 projects and asserting the overflow series
- [ ] 3.4 Feed the build-info gauge from `internal/version` so its labels match `GET /api/version`; verify with a unit test comparing both against the same version string

## 4. Instrumentation

- [ ] 4.1 Record the terminal status counter and the duration observation at the `FinishRun` and `CancelRun` call sites; verify with unit tests asserting exactly one increment per terminal transition, including the cancel-of-queued-run path
- [ ] 4.2 Expose a `Counts()` accessor on the scheduler in `internal/service/run_concurrency.go` and read `running`/`queued` gauges from it on scrape; verify with a unit test that queues runs past the concurrency limit and asserts both gauges, then drains and asserts zero
- [ ] 4.3 Record webhook delivery outcomes (accepted, rejected, skipped) in the webhook handler; verify with unit tests over each outcome

## 5. Endpoint and access control

- [ ] 5.1 Register `GET /metrics` in `buildRouter` only when metrics are enabled; verify with unit tests asserting `404` when disabled and `200`/`401` when enabled
- [ ] 5.2 Implement the metrics auth middleware: constant-time scrape-token comparison first, then `RequireAuth` + `RequireRole(viewer)`, else `401` with a body identical to the no-credential case; verify with unit tests for each branch, including a wrong token
- [ ] 5.3 Assert the scrape token authenticates nothing else: presenting it to an `/api` route returns `401` and resolves no account; verify with a unit test per route group
- [ ] 5.4 Assert the response content type is `text/plain; version=0.0.4; charset=utf-8`; verify with a unit test

## 6. Verification

- [ ] 6.1 Run `make test` and `make test-coverage`; verify the suite is green and unit coverage is still 100%
- [ ] 6.2 Run `make lint` and `glab ci lint`; verify both are clean
- [ ] 6.3 Manual end-to-end on a live instance: start with metrics disabled and confirm `404`; restart enabled with a token, scrape with and without the token, trigger a run and a webhook delivery, and confirm the counters, the queue gauge, and the histogram move; pipe the page through `promtool check metrics` and confirm it parses
- [ ] 6.4 Manual check of the log stream: confirm every line of a full run is valid JSON, that `chiMiddleware.Logger` lines share the format, and that a failing credential resolution logs the redaction placeholder
