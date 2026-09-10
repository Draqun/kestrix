## Context

See proposal.md - Why. The constraints that shape this design:

- `internal/web/handler_webhook.go:55-70` does verification inline: an
  `!=` comparison of `X-Gitlab-Token`, only when `s.cfg.Signature.Enabled &&
  Secret != ""`, then `io.ReadAll(r.Body)` with no cap, then
  `ParseGitLabPushEvent`. The project is looked up afterwards at line 89 by
  `event.GitLabProjectID`, so the only place a per-project secret could be
  consulted is after the body has been read and parsed.
- `internal/web/webhook_verify.go` already has constant-time `gitlab` and
  `hmac` middleware plus `SignatureConfig.Middleware()`. It is tested and
  mounted nowhere; `buildRouter` at `server.go:350` registers the bare
  handler. `cmd/kestrix/main.go:285-289` sets `Enabled: cfg.Webhook.Secret != ""`,
  which is the fail-open switch.
- `projects.webhook_secret TEXT NOT NULL DEFAULT ''` exists
  (`pgstore.go:85`) and is read by `FindByGitLabID`
  (`pgstore_project.go:68`). `domain.ProjectConfig.WebhookSecret` has
  `json:"webhook_secret,omitempty"` (`ports.go:312`) and is encoded straight to
  the wire at `handler_project.go:140` and `:317`.
- `ProjectService.UpdateValidated` (`service.go:93`) replaces the whole row from
  the request struct, so an update that omits `webhook_secret` currently
  clears it.
- `webhook-log-history` requires that a request rejected before its payload is
  parsed is never recorded (`Event Recording`). Whatever verifies must run
  before the first `s.logWebhook`.
- `access-control` "Webhook delivery stays outside user authentication" fixes
  that `/webhook` never sees `RequireAuth`; nothing here changes that.
- `login_throttle.go:189` already reads and re-buffers the login body, so a
  cap must sit in front of it, not inside `handleLogin`.
- Ten handlers call `json.NewDecoder(r.Body).Decode` directly; there is no
  shared decode helper. `errwrite.go` holds the one shared response helper.
- Nothing streams: no `Flusher`, no SSE, no hijack. Run progress is polled by
  the UI. A `WriteTimeout` therefore cannot cut a legitimate response, except
  the webhook path, which acquires a Git source before answering.
- `scripts/test.sh:67` holds `internal/web` at 100% coverage.

## Goals / Non-Goals

**Goals:**

- One verification path for `/webhook`, in one place, constant time, honouring
  the configured method, and unable to be disabled by leaving a variable empty.
- The per-project secret is the primary secret; the global one is a fallback,
  and the rejection never says which was consulted.
- Bounded connections and bodies on every route with two integers and three
  durations an operator can read off the environment.
- `repo_url` cannot name a filesystem, a plaintext transport, or the host's own
  loopback and link-local ranges.

**Non-Goals:**

- Changing the webhook payload contract or response shape beyond the new
  `401`/`413` rejections.
- A migration for the `webhook_secret` column: it already exists with the
  right type and default.
- Egress control at fetch time (DNS resolution, firewalling `git`); see D6.

## Architecture

```
  POST /webhook
     │
     ▼
  MaxBody(KESTRIX_WEBHOOK_MAX_BODY_BYTES) ──── too large ──▶ 413 (not recorded)
     │
     ▼
  verifyDelivery(body, headers)
     │  1 read body once (capped)
     │  2 probe {project:{id}} only          ┌──────────────┐
     │  3 project secret ◀── FindByGitLabID ─│ ProjectStore │
     │  4 verify(method, projectSecret)      └──────────────┘
     │    ‖ verify(method, globalSecret)
     │  5 neither / none configured ─────────────────────────▶ 401 (not recorded)
     ▼
  ParseGitLabPushEvent(body)  ── invalid ──▶ 400 (not recorded)
     │
     ▼
  existing flow: lookup ▶ load pipeline ▶ resolve ▶ dispatch ▶ record


  all /api routes
     │
     ▼
  MaxBody(KESTRIX_HTTP_MAX_BODY_BYTES) ──▶ throttleLogin ▶ RequireAuth ▶ handlers
                                                          └─ decodeJSON(): *MaxBytesError ▶ 413


  http.Server{ ReadTimeout, WriteTimeout, IdleTimeout, Handler: router }
```

## Decisions

### D1. Verification is a step inside the handler, not a router middleware

The natural fix is to mount `SignatureConfig.Middleware()` on `/webhook`. It
cannot know the per-project secret: the project ID is inside the body. Two
options were considered.

*Middleware that parses the body twice* - once for the ID, once in the handler.
Rejected: the body would be buffered and re-wrapped in `io.NopCloser` exactly
as `VerifyHMACSignature` and `throttleLogin` already do, adding a third copy of
that pattern and a hidden ordering dependency between two layers.

*Chosen*: `handleWebhook` reads the capped body once, calls a pure
`verifyDelivery(method, headers, body, projectSecret, globalSecret) bool`, then
parses. `webhook_verify.go` keeps its constant-time primitives (`hmac.Equal`,
HMAC-SHA256) and loses its middleware constructors and `SignatureConfig.Enabled`.
The `Enabled` flag is what made fail-open possible; removing the flag removes the
mode.

The project-ID probe decodes only `{"project":{"id":N}}` into a two-field
struct. A body that is not JSON, or has no ID, yields ID `0` and therefore no
project secret; the global secret is then the only thing that can accept it.
That is the same outcome the full parser would reach, without trusting anything
else in the body first.

`verifyDelivery` and the probe struct are the seam `multi-source-projects`
builds on: that change widens the probe to also read the repository URLs,
resolves the set of matching projects, and calls `verifyDelivery` once per
match with that project's secret (see its design D6). The rule stays the
same in both positions - a project whose verification fails is not processed
and writes no history row, and a delivery no project or global secret accepts
answers `401`.

### D2. Accept if either secret verifies; the project secret is consulted first

A delivery for project *P* is accepted when it verifies against *P*'s secret
(if *P* is registered and has one) or against `KESTRIX_WEBHOOK_SECRET` (if
set). Both are evaluated with constant-time comparison; the project secret is
tried first only so the common case short-circuits, not for security.

Alternative: project secret, when set, *replaces* the global one. Rejected -
it makes the global secret's meaning depend on per-row state an operator
cannot see from the environment, and gains nothing: whoever holds the global
secret is already trusted for every project.

Alternative: require a per-project secret for every registered project and
drop the global one. Rejected for now - single-pipeline mode
(`handler_webhook.go:143`, no `projectStore` or ID `0`) has no project row to
hold a secret, and existing deployments configured one secret in GitLab per
Kestrix instance. It is the right end state; the `has_webhook_secret` flag
added here makes the gap visible so a later change can require it.

### D3. Every rejection is the same `401`

Unknown project with no global secret, known project with a wrong token, and
known project with no secret of its own and no global secret all answer
`401 {"error":"unauthorized"}`. A distinct message would turn `/webhook` into an
oracle for which GitLab project IDs are registered - the exact thing a forger
needs next. The `ignored` `200` path survives only behind a verified global
secret, where the caller is already trusted (proposal scenario "Unknown project
with a valid global secret is still ignored").

`401` rather than `403`: the existing inline check already uses `401`, GitLab's
webhook log shows the status, and the delivery lacks credentials rather than
holding insufficient ones.

### D4. `webhook_secret` becomes write-only, with keep-on-empty update semantics

`ProjectConfig` gains `HasWebhookSecret bool json:"has_webhook_secret"` and
`WebhookSecret` becomes `json:"webhook_secret,omitempty"` on input only - the
response path is a copy with the field blanked. `handler_project.go` writes
`ProjectConfig` at two sites and a `req` echo at `:118`; a single
`publicProject(cfg) ProjectConfig` helper is applied at all three, mirroring how
credential responses already strip secret fields.

`UpdateValidated` copies the stored secret onto `cfg` when the request's is
empty. The project form in the UI never sent the field (no `webhook_secret` in
`ui/src`), so today every UI save silently wipes a secret set via the API; this
also fixes that. Clearing a secret needs a distinct signal and is out of scope.

Alternative: a dedicated `PUT /api/projects/{name}/webhook-secret` route.
Rejected as a new surface for a value that already has a home in the create
payload; it can be added later if rotation needs an audit trail of its own.

### D5. Server timeouts and two body bounds, all in `config.Environment`

`Server.ListenAndServe` builds an `http.Server{ReadTimeout, WriteTimeout,
IdleTimeout}` instead of calling the package function; the `listenAndServe`
test seam becomes a seam over `*http.Server`. Defaults: read `30s`, write
`120s`, idle `120s`. `ReadHeaderTimeout` is left to inherit `ReadTimeout`.
`MaxHeaderBytes` stays at the stdlib 1 MiB.

Write `120s` is a compromise: every API handler answers in milliseconds, but
`/webhook` runs `loadProjectPipeline`, a Git fetch, before responding. A
mirror already in the source cache refreshes in seconds; a cold clone of a
large repository can exceed a minute. Cutting the response there would make
GitLab retry and Kestrix fetch twice. The value is configurable precisely so
an operator with slow remotes can raise it.

Body bounds: `KESTRIX_HTTP_MAX_BODY_BYTES` default `1 MiB` on the `/api`
subtree; `KESTRIX_WEBHOOK_MAX_BODY_BYTES` default `8 MiB` on `/webhook`.
GitLab caps push payloads at twenty commits, but each commit carries full
`added`/`modified`/`removed` file lists, so a squash-heavy monorepo push can
run to megabytes. One `maxBody(n)` middleware wraps `http.MaxBytesReader`
around `r.Body`; a `decodeJSON(w, r, v) bool` helper in `errwrite.go`
replaces the ten inline decoders and maps `*http.MaxBytesError` to `413`.
`handleWebhook` and `throttleLogin` read raw bytes and check the same error
type.

Alternative: one bound for everything. Rejected - `1 MiB` is too small for
the webhook and `8 MiB` is too generous for a login form.

### D6. `repo_url` is validated syntactically at registration time

`validateProject` gains a scheme allow-list (`https`, `ssh`) plus the scp-like
form `[user@]host:path` (recognised when there is no `://` and the first `:`
precedes any `/`), and a literal-host deny-list: loopback (`127.0.0.0/8`,
`::1`, `localhost`), link-local (`169.254.0.0/16`, `fe80::/10`), and the
unspecified address. `net/netip` does the range checks; no dependency.

DNS is deliberately not resolved. Resolving at registration time is racy
(the record can change before the first fetch) and turns project creation into
a network call with its own timeout and failure mode. The realistic target of
this check is the cloud metadata endpoint at `169.254.169.254`, which is
reached by literal address; a hostname that resolves there requires the
attacker to control DNS the Kestrix host trusts, at which point `repo_url` is
not the weakest link. RFC 1918 stays allowed because self-hosted GitLab is the
primary deployment story.

`http://` is refused outright rather than warned: a plaintext transport carries
the credential store's tokens on the wire.

## Security considerations

- Fail-closed is the whole point: there is no configuration in which `/webhook`
  accepts an unverified delivery. `Enabled` is deleted rather than defaulted
  to true, so it cannot regress by omission.
- All secret comparison goes through `hmac.Equal`. The `!=` at
  `handler_webhook.go:56` is removed.
- Rejections are uniform (D3) and unrecorded, so the history retention bound
  cannot be driven by an anonymous caller and registration cannot be
  enumerated.
- The per-project secret never leaves the server (D4). Logs already avoid
  secrets per `rbac-hardening` decision logging; the new verification logs
  project name and outcome only.
- Body caps and timeouts bound memory and connection slots per client; they do
  not rate-limit, which stays a non-goal.
- The Compose `dev-secret` default is removed. A Compose deployment without
  `KESTRIX_WEBHOOK_SECRET` in `.env` now rejects deliveries, which is loud
  rather than silently forgeable.

## Risks / Trade-offs

- [Existing deployments with no secret stop receiving webhooks] → CHANGELOG
  marks it BREAKING; `.env.template` and the Compose comment tell the operator
  to set `KESTRIX_WEBHOOK_SECRET` or a per-project secret; `GET /api/health`
  is unaffected so the outage is visible in the webhook log, not as downtime.
- [A per-project secret set via API was being wiped by UI saves; after D4 it
  persists, which may surprise someone who relied on the wipe] → Documented
  in `docs/webhook-receiver.md`; the flag `has_webhook_secret` shows the
  state.
- [Write timeout cuts a slow cold clone on the webhook path] → `120s`
  default, configurable; the failure is a GitLab retry, not data loss. Moving
  acquisition off the request path is a separate change.
- [Legitimate push payload over 8 MiB] → configurable; GitLab logs the `413`.
- [`repo_url` check refuses a hostname someone legitimately serves on a
  loopback for testing] → the Docker E2E uses a bare remote on a bridge
  address, not loopback; document `host.docker.internal`-style alternatives.

## Migration Plan

1. Deploy the binary. No schema change.
2. Set `KESTRIX_WEBHOOK_SECRET` in `.env` (or per-project secrets via `PUT
   /api/projects/{name}`) before or with the deploy; GitLab retries failed
   deliveries, so a short window of `401` is recoverable from GitLab's webhook
   log.
3. Optionally set `KESTRIX_HTTP_*` knobs; defaults apply otherwise.
4. Rollback: previous binary, same database. Secrets stored meanwhile are
   simply unused again.

## Open Questions

None that change the specs. Whether to require a per-project secret and retire
the global one (D2) is left for a follow-up once `has_webhook_secret` shows
adoption.
