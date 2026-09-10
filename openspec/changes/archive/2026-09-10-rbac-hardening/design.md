## Context

See proposal.md — Why. The constraints that shape this design:

- `internal/web/middleware.go:25-47` (`RequireAuth`) verifies the token and puts
  `domain.Claims` on the context. It never touches a store, so the global role
  it publishes is whatever the token said when it was signed.
- `internal/web/middleware.go:57-83` (`RequireRole`) resolves the per-project
  grant from `GrantStore` on every request, then takes
  `domain.HigherRole(claims.Role, grant)`. Grants are live; the global role is
  not.
- `internal/auth/token.go:14` fixes `TokenTTL = time.Hour`, and `rbac-auth`
  design D7 rules out refresh tokens. That hour is therefore the current upper
  bound on how long a revoked privilege survives.
- Schema is created by the `CREATE TABLE IF NOT EXISTS` block in
  `internal/store/pgstore.go:110-142`. There is no migration tool, so a new
  column joins the same idempotent block as an `ALTER TABLE ... ADD COLUMN IF
  NOT EXISTS` statement in the `statements` slice below it.
- `internal/web/handler_pipeline.go` reads a caller-supplied directory at lines
  24, 47, 96, 151, and 197, each falling back to `s.cfg.PipelineDir`.
- `scripts/test.sh:60` enforces `WEB_COVERAGE_THRESHOLD=100` and 100% on
  `internal/service`. Every new middleware branch needs a test.
- `internal/web/server.go:245-352` builds the router. Middleware is the only
  place a rule can be added without touching 28 handlers.

## Goals / Non-Goals

**Goals:**

- One freshness rule for the whole effective role: global part and grant part
  are read at the same moment, from the same request.
- A store failure is visible as a failure, never as a quieter set of
  permissions.
- Revocation takes effect in seconds, bounded by a number an operator can read
  off a configuration value.
- No new runtime dependency, no external cache, no migration tool.

**Non-Goals:**

- Making the route→permission mapping data-driven. It stays code, per
  `rbac-auth` D1.
- Sharing throttle or cache state between processes. A single `kestrix serve`
  is the deployment unit today.
- Reworking the token format beyond one added claim.

## Architecture

```
  request ──▶ SecurityHeaders ──▶ /api ──┬── public: /health, /auth/login
                                         │        │
                                         │        └─▶ LoginThrottle ──▶ handleLogin
                                         │
                                         └── protected
                                              │
                                    ┌─────────▼──────────┐
                                    │ RequireAuth        │
                                    │  1 verify JWT      │──▶ TokenIssuer
                                    │  2 load identity   │──▶ IdentityCache
                                    │  3 check version   │       │ miss
                                    │  4 publish Claims  │       ▼
                                    └─────────┬──────────┘    UserStore
                                              │                (pgstore_user)
                                    ┌─────────▼──────────┐
                                    │ RequireRole(min)   │──▶ GrantStore
                                    │  effective =       │     (pgstore_grant)
                                    │   max(role, grant) │
                                    │  store error ▶ 503 │
                                    └─────────┬──────────┘
                                              ▼
                                    handlers ──▶ boundPipelineDir(dir)
                                                   outside root ▶ 403

  writes that revoke:  update role │ update password │ delete account
                              └────────┴─────────────┴──▶ token_version++
                                                          + cache invalidate
```

## Decisions

### D1. The store is the authority on the global role; the token proves identity

`RequireAuth` loads the account after verifying the signature and builds
`domain.Claims` from the stored record, not from the token body. The token's
`role` claim is dropped from the decision path entirely.

Alternative considered: keep reading the role from the token and shorten
`TokenTTL` to a few minutes. Rejected — it trades one unbounded staleness
window for a smaller one, forces a re-login cadence users notice, and still
cannot revoke a deleted account.

Consequence: the token is no longer self-sufficient. Every protected request
depends on the user store being reachable, which D3 turns into an explicit
failure mode rather than a silent one.

### D2. `token_version` is the revocation signal

`users` gains `token_version INT NOT NULL DEFAULT 1`. `Issue` copies the current
value into a `ver` claim; `RequireAuth` rejects a token whose `ver` does not
equal the stored value with `401`. Changing a role, changing a password, and
deleting an account each increment it.

D1 alone already fixes role staleness — the role is re-read anyway. `ver`
exists for what a re-read cannot express: "this specific token is finished."
It revokes a session after a password change without waiting for expiry, and it
lets an administrator force a re-login without deleting the account.

A token with no `ver` claim is rejected. That is the breaking part of this
change, and it is deliberate: accepting a versionless token would leave a
one-hour window in which the old, unrevocable tokens still work.

### D3. Authorization store failures fail closed with `503`

`RequireRole` currently writes `if ... err == nil && found`. That treats "the
database is down" the same as "no grant exists", which quietly demotes a caller
and returns a `403` that blames the caller for an outage.

Both lookups — identity in `RequireAuth`, grant in `RequireRole` — return
`503 Service Unavailable` on a store error, with the underlying error logged and
not returned to the caller. A missing account remains `401`; a missing grant
remains "no grant", which is not an error.

Fail-open was never on the table. Fail-closed with `403` was: rejected because
`403` tells an operator to check permissions when they should be checking the
database.

### D4. Identity cache: small, short, and invalidated on write

D1 adds one `SELECT` per protected request. A cache keyed by account name holds
`{role, token_version, exists}` for `KESTRIX_IDENTITY_CACHE_TTL` (default `5s`,
`0` disables), behind a `domain.IdentityCache` port with an in-memory adapter.
Every write that bumps `token_version` also evicts that key, so the TTL only
bounds staleness caused by another process, never by this one.

Negative results are cached too — otherwise a token for a deleted account
becomes a free database query per request until it expires.

Alternative considered: cache nothing. Rejected on the request-per-page cost of
the dashboard, not on benchmark evidence; the TTL is configurable precisely so
an operator who disagrees can set `0`.

Alternative considered: cache the whole authorization decision including the
grant. Rejected — the cache key would have to include the project and the route,
and a stale entry would then grant access to a project rather than merely
report a stale role.

### D5. Pipeline directory is confined to a configured root

`boundPipelineDir(requested string) (string, error)` resolves the request's
`dir` with `filepath.Abs` plus `filepath.EvalSymlinks` and requires the result
to be `s.cfg.PipelineDir` or below it. All five call sites in
`internal/web/handler_pipeline.go` use it. An empty `dir` keeps the existing
default. A path outside the root is `403`, not `400`: it is a permission answer,
and `400` invites probing for which paths parse.

Alternative considered: drop the `dir` parameter. Rejected — `POST /api/resolve`
and `POST /api/generate` are documented with it and the UI sends it.

`EvalSymlinks` runs before the prefix test so a symlink inside the root cannot
point out of it. A `dir` that does not exist fails the same way as one outside
the root, so the check does not become a directory-existence oracle.

### D6. Read visibility stays global, and is now stated

`GET /api/projects` returns every project to every authenticated account. This
change makes that a written requirement rather than an omission.

It follows from `rbac-auth` D3: a grant raises a role and never lowers one, so
a global `viewer` is a viewer *everywhere*. Filtering lists by grant would make
the grant subtractive for reads and additive for writes — two meanings for one
record.

Accepted risk: an account with the lowest role enumerates every project name,
run, and webhook event. For a CI control plane whose accounts are all created by
an administrator with a CLI, that is the intended blast radius. Changing it is a
`project-visibility` capability, not a patch to this middleware, because it
needs a filtering contract on `ProjectStore`, `RunStore`, and the dashboard
aggregate.

### D7. Login throttling is per process, in memory, and account-blind in its answer

A fixed-window counter keyed separately by account name and by client address
(`X-Forwarded-For` first hop when `KESTRIX_TRUST_PROXY_HEADER` is set, otherwise
`RemoteAddr`). Over `KESTRIX_LOGIN_MAX_ATTEMPTS` (default 10) failures in
`KESTRIX_LOGIN_WINDOW` (default 1m), further attempts get `429` with
`Retry-After` until the window rolls.

The account key stops a slow attack on one password; the address key stops a
fast attack across many accounts. Throttling on the address alone leaves a
distributed guess unthrottled; throttling on the account alone lets one client
sweep the whole account list.

A successful login clears the account counter. A throttled request never reaches
`AuthService`, so it costs no argon2id work — the throttle is also the
protection against argon2id being used as the amplifier.

`429` for a throttled account is observable, so it discloses that the account is
under attack, not whether it exists: an unknown name is throttled identically.

### D8. Security headers are a middleware, not a deployment note

`rbac-auth` D8 keeps the token in `sessionStorage` and accepted XSS exposure as
the trade. The mitigation was never implemented. A `SecurityHeaders` middleware
on the root router sets `Content-Security-Policy`,
`X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, and
`X-Frame-Options: DENY`.

The policy is `default-src 'self'` with the `style-src 'self' 'unsafe-inline'`
that the bundled UI needs, and is overridable through
`KESTRIX_CONTENT_SECURITY_POLICY` for deployments that front the UI elsewhere.
Swagger UI at `/docs/*` gets its own relaxed policy rather than loosening the
application's.

## Security considerations

- **Revocation latency** is now `KESTRIX_IDENTITY_CACHE_TTL` (default 5s) rather
  than `TokenTTL` (1h), and 0s within the process that made the change. This is
  the headline result of the change and belongs in the deployment docs.
- **Availability is now part of authorization.** D1 makes every protected
  request depend on the user store. D3 makes that dependency loud. A store
  outage that previously degraded to stale-but-working access now returns `503`
  — the correct answer, and one the runbook must name.
- **The identity cache is a privilege cache.** Its TTL is an upper bound on
  privilege staleness. It is keyed by account name only, never by token, so two
  tokens for one account cannot disagree.
- **Timing.** The `token_version` comparison and the identity lookup run after
  signature verification, so an unauthenticated attacker cannot use them to
  probe which accounts exist. The `401` for a bad signature, a bad version, and
  a missing account are one message.
- **Log content.** Denials log account name, route, required role, and effective
  role. They MUST NOT log the token, any part of it, or the request body —
  `writeError` already returns generic text and stays that way.
- **The throttle is per process.** Two replicas behind a load balancer double the
  allowance. That is a documented limit, not a defect to be worked around with a
  shared store in this change.
- **`X-Forwarded-For` is only trusted when configured.** Reading it
  unconditionally would let any client forge its own throttle key, which is
  strictly worse than throttling on the shared proxy address.
- **CSP does not replace the storage decision.** It reduces the chance a script
  reaches `sessionStorage`; it does not make the token safe there. Moving to a
  `HttpOnly` cookie remains a separate, larger change with its own CSRF work.

## Migration

One column, added idempotently:

```sql
ALTER TABLE users ADD COLUMN IF NOT EXISTS token_version INT NOT NULL DEFAULT 1;
```

Existing accounts start at version 1 and keep working. Existing *tokens* do not:
they carry no `ver` claim and are rejected. Operators are told to expect one
forced re-login on upgrade. There is no rollback concern — an older binary
ignores the column and the claim.

## Open questions

None. D6 records the one judgement call (global read visibility) as a decision
with a named follow-up capability rather than leaving it open.
