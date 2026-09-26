## Why

Kestrix has no machine-readable runtime signal and no structured logs. An
operator who wants to know whether runs are failing, how deep the queue is, or
how long a run takes has to read the UI or `log.Printf` lines that carry no
level, no timestamp beyond the stdlib prefix, and no stable fields. `docs/deployment.md`
tells an operator to run Kestrix behind Caddy in production, but gives them
nothing to alert on. The 28 `log.Printf` call sites (20 in `internal/web`, 7 in
`internal/service`, 1 in `internal/store`) cannot be filtered, so the only way to
quiet or deepen logging is a code change.

## What Changes

- New `GET /metrics` endpoint in Prometheus text exposition format, covering:
  run counts by terminal status, current running and queued run counts, a run
  duration histogram, webhook deliveries by outcome, and a build-info gauge
  carrying the version already exposed by `GET /api/version`.
- The endpoint is opt-in (`KESTRIX_METRICS_ENABLED`, default off) and, when
  enabled, is authenticated: a dedicated scrape token (`KESTRIX_METRICS_TOKEN`)
  or an ordinary signed-in viewer session. It is never a fourth public route.
- All 28 `log.Printf` call sites move to `log/slog` with a JSON handler and
  stable field names (`run_id`, `project`, `account`, `route`, `err`). Secret
  redaction already applied to run output SHALL also apply to log fields.
- New `KESTRIX_LOG_LEVEL` / `logging.level` setting (default `info`) following
  the established flag > file > env > default precedence.
- **BREAKING** for anything parsing Kestrix stdout: the line format changes
  from `log.Printf` free text to JSON.

## Capabilities

### New Capabilities
- `observability`: what Kestrix exposes about its own runtime — the metrics
  endpoint, its metric set, its exposure rules, and the structured log contract.

### Modified Capabilities
- `runtime-configuration`: `KESTRIX_METRICS_ENABLED`, `KESTRIX_METRICS_TOKEN`
  and `KESTRIX_LOG_LEVEL` join the settings bound by the deterministic
  precedence requirement.
- `access-control`: `/metrics` is a new route outside `/api`, so it falls
  outside the existing public/protected route groups and gains its own
  authentication requirement in that spec.

## Non-goals

- No tracing, no OpenTelemetry, no metrics push gateway.
- No bundled Prometheus, Grafana, or dashboards in `docker/`.
- No per-task or per-account metric cardinality — project name is the only
  label with unbounded values, and it is bounded by a documented cap.
- No change to `GET /api/logs`, which serves pipeline run output, not process logs.

## Impact

`internal/web` (router, new handler, middleware), `internal/service`,
`internal/store`, `internal/config` (three settings), `cmd/kestrix` (precedence
table, slog handler installation), `docs/deployment.md` and `README.md`
(scrape setup), `.env.template`. No database migration. Dependency choice
(hand-rolled exposition vs `prometheus/client_golang`) is settled in design.md.
