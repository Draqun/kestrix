## Context

See proposal.md — Why. Constraints that shape the approach:

- `internal/web`, `internal/service`, `internal/store` sit behind ports in
  `internal/domain/ports.go`; run state transitions all pass through the
  `RunStore` port (`CreateRun`, `FinishRun`, `CancelRun`, `SetRunQueued`,
  `StartQueuedRun`, `FailInterruptedRuns`).
- The router (`internal/web/server.go:402` `buildRouter`) already splits `/api`
  into a public group and a protected group behind `RequireAuth` +
  `RequireRole`. `/metrics` is outside `/api`, so `maxBody` and those groups do
  not cover it.
- The repository has a hard 100% unit coverage gate (`make test-coverage`).
  Every branch added here needs a unit test, which argues for a small, testable
  surface over a large vendored one.
- Direct dependencies today are 10 modules. `log/slog` is stdlib; Prometheus
  client libraries are not.
- Secret redaction already exists in `internal/service/redact.go` and is applied
  to run output.

```
            scrape                      browser (viewer session)
               |                                   |
               v                                   v
        +-------------------------------------------------+
        |  GET /metrics   (registered only when enabled)   |
        |  auth: constant-time scrape token OR RequireAuth |
        +-------------------------------------------------+
                                |
                                v
                  +---------------------------+
                  |  internal/metrics         |
                  |  registry (counters,      |
                  |  gauges, one histogram)   |
                  +---------------------------+
                       ^            ^        ^
            run events |   webhook  |        | build info
                       |   outcomes |        |
        internal/service     internal/web    internal/version
        (RunStore-adjacent   (webhook        (already feeds
         call sites)          handler)        GET /api/version)
```

## Goals / Non-Goals

**Goals:**

- One in-process registry with no I/O, so the handler is a pure render.
- Instrumentation at the points that already own a state transition, so a
  counter cannot drift from the database.
- Logging migration that is mechanical and reviewable in one pass.

**Non-Goals:**

- Metric persistence across restarts. Counters restart at zero; Prometheus
  handles resets.
- Per-task, per-account, or per-run-id labels.
- Replacing `chiMiddleware.Logger` with a request-metrics middleware in this
  change (request-rate metrics are not in the spec).

## Decisions

### D1: Hand-rolled exposition, no `prometheus/client_golang`

The metric set in the spec is five families: three counters, two gauges, one
histogram, one info gauge. The exposition format for those is a few dozen lines
of `fmt.Fprintf`. `client_golang` pulls in `prometheus/common`,
`prometheus/procfs`, `cespare/xxhash`, and `protobuf` — a large surface to hold
at 100% unit coverage, and an odd weight for a project with 10 direct deps.

**Alternative considered:** `client_golang`, which would give correct escaping,
a battle-tested histogram, and the `/metrics` handler for free. Revisit it the
first time this change needs exemplars, native histograms, or the Go collector.
The metric names chosen here are the conventional ones, so a later swap is a
drop-in on the collection side.

**Consequence:** label-value escaping and `HELP`/`TYPE` formatting are our
problem; one table-driven test over the renderer covers it, and project names
must be escaped because they reach a label.

### D2: Registry is a plain struct behind a small interface, injected

`internal/metrics` exposes a `Recorder` interface (`RunFinished(status string,
d time.Duration)`, `RunsInFlight(running, queued int)`, `WebhookDelivery(outcome
string)`) and a no-op implementation used when metrics are disabled. The
existing server construction seam (`web.NewServerE` / `NewServerEWithDeps`,
already used by `orphan-run-recovery`) carries it, so no package reaches for a
global.

**Alternative considered:** a package-level singleton. Rejected: tests run in
parallel and a global counter makes them order-dependent.

### D3: Gauges are read, not tracked

`running` and `queued` counts are derived on scrape from the scheduler's own
state (`internal/service/run_concurrency.go`), not incremented and decremented
at call sites. An increment/decrement pair can leak on an error path; a read
cannot. Terminal counters and the duration histogram are incremented at
`FinishRun`/`CancelRun` call sites, where the transition is already guarded.

### D4: Project label cap

The project label is capped (default 100 distinct values, a constant, not a
setting). Beyond the cap, values collapse to `project="__other__"`. This keeps a
multi-tenant instance from turning a scrape into an unbounded time series set.

### D5: Metrics auth is its own middleware, not `RequireAuth` alone

`RequireAuth` resolves an account; a scraper has none. The endpoint therefore
runs a dedicated middleware: if a scrape token is configured and the bearer
token matches under `subtle.ConstantTimeCompare`, serve; otherwise fall through
to the ordinary `RequireAuth` + `RequireRole(viewer)` chain; otherwise `401`.
The token is never resolved to an account, so it cannot authenticate `/api`.

### D6: Route is registered only when enabled

Registration is conditional in `buildRouter`, so a disabled instance returns the
router's ordinary `404`. No handler exists to leak timing or headers.

### D7: slog handler installed once at startup, `log` redirected

`cmd/kestrix` builds a `slog.JSONHandler` at the configured level and installs
it with `slog.SetDefault`, then routes the stdlib logger into it with
`slog.SetLogLoggerLevel` + `log.SetOutput`, so `chiMiddleware.Logger` (stdlib
`log`) lands in the same stream instead of splitting the format. The 28
`log.Printf` sites are rewritten to `slog.Warn`/`slog.Error`/`slog.Info` with
key/value fields; `err` carries `error.Error()` after passing through the
existing redactor.

### D8: Redaction lives in a new `internal/redact` package, below `internal/service`

Log fields (this change) and run output (existing) both need secret
redaction, and `internal/store` now needs it too - `pgstore_run.go` redacts
before persisting, and `internal/store` must not import `internal/service`,
which sits above it in this repo's hexagonal layering. The redaction
primitive (`Values`/`Prepare`/`Err`) moved into a new `internal/redact`
package with no dependency on `service`, `store`, or `web`, so every layer
can import it directly; `internal/service/redact.go` keeps a thin delegator
for its existing call sites. See REVIEW.md R5.

## Risks / Trade-offs

- **Hand-rolled exposition is subtly wrong (bad escaping, wrong type line) and a
  scraper rejects the page** → one test asserts the whole rendered page against
  a golden fixture, and the manual verification step scrapes it with
  `promtool check metrics`.
- **Enabling metrics widens the attack surface of an instance behind Caddy** →
  off by default, authenticated when on, `404` when off, constant-time token
  comparison, and `docs/deployment.md` documents scraping over the internal
  Compose network rather than the public listener.
- **The JSON log switch breaks an operator's existing `grep`** → called out as
  **BREAKING** in the changelog, and the level setting lets them raise or lower
  volume without a rebuild.
- **Redaction misses a field and a secret reaches a log line** → log fields,
  run output, and stored run data go through the same `internal/redact`
  primitive (see D8); a unit test feeds a known secret through every new
  call site that carries an `err`.
- **Deriving gauges on scrape ties the handler to scheduler internals** → the
  scheduler exposes a single `Counts()` accessor; the handler does not reach
  into its fields.

## Migration Plan

1. Ship with `metrics.enabled=false`. An existing deployment sees no new route.
2. The logging change takes effect on upgrade with no setting. An operator who
   parses stdout should pin the previous image until they adapt.
3. Rollback is the previous image; no schema, no persisted state, no data
   migration.
4. Enabling metrics later is a setting plus a restart, with no other change.
