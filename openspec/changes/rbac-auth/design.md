## Context

See proposal.md — Why. The constraints that shape this design:

- `internal/web/server.go:209-262` builds one Chi router. All 28 `/api` routes
  are registered flat inside a single `r.Route("/api", ...)`, with only
  `RequestID`, `Logger`, `Recoverer` and `Heartbeat` as middleware. There is no
  place where a request is associated with a caller.
- Ports live in `internal/domain/ports.go`; PostgreSQL adapters implement them
  in `internal/store`. `internal/service` holds use-case logic. New identity
  code has to follow that split to pass `internal/domain/portscheck`.
- Schema is created by `CREATE TABLE IF NOT EXISTS` statements in
  `internal/store/pgstore.go:63-108`. There is no migration tool, so new tables
  join the same idempotent block.
- `internal/credentials/key.go` already defines the file-backed key pattern the
  master key uses, validated before the listener opens
  (`internal/web/server.go:185-191`).
- `scripts/test.sh:60` enforces `WEB_COVERAGE_THRESHOLD=100` and 100% on
  `internal/service`. Every new handler and middleware branch needs a test.
- Direct dependencies are minimal (`go.mod`): Chi, HCL, pgx, swaggo, go-cty,
  yaml. There is no crypto or JWT dependency yet.

## Goals / Non-Goals

**Goals:**

- One enforcement point that no future route can bypass by accident.
- Identity resolution isolated behind a port, so adding GitLab OIDC later
  touches one adapter and no handler.
- The server has no unauthenticated mode, not even a first-run window.

**Non-Goals:**

- Migrating existing deployments' data. There are no users today, so there is
  nothing to convert; operators create the first account with the CLI.
- Making authorization data-driven. The route→permission mapping is code.

## Architecture

```
  UI (React)                        kestrix serve
  ─────────                         ─────────────────────────────────────────
  login form ── POST /api/auth/login ─▶ handler_auth ─▶ AuthService
                ◀── JWT ──────────────                    │
                                                          ├─▶ UserStore (port)
  every call ── Authorization: Bearer <jwt> ─┐            │      └─ pgstore_user
                                             ▼            ├─▶ PasswordHasher (port)
                                    ┌──────────────────┐  │      └─ argon2id
                                    │ RequireAuth      │  └─▶ TokenIssuer (port)
                                    │  verify + claims │         └─ HMAC / file key
                                    └────────┬─────────┘
                                             ▼
                                    ┌──────────────────┐
                                    │ RequireRole(...) │──▶ GrantStore (port)
                                    │  global + grant  │      └─ pgstore_grant
                                    └────────┬─────────┘
                                             ▼
                                    existing /api handlers

  POST /webhook ──▶ VerifyGitLabSignature (unchanged, shared secret, no user)
```

## Decisions

### D1. Enforcement is router middleware, not per-handler checks

`RequireAuth` wraps the whole `/api` subrouter; `RequireRole` wraps route
groups. Handlers stay unaware of identity except where they need the caller's
name.

Alternative considered: a check at the top of each handler. Rejected — 28
handlers means 28 chances to forget one, and a new route added later gets no
protection by default. With subrouter middleware, a new route is protected
unless someone deliberately registers it outside the group.

The public routes (`/api/health`, `/api/auth/login`) are registered on their own
subrouter *outside* the protected one, so the protected group has no exception
list to keep in sync.

### D2. Identity behind three narrow ports, not one `Authenticator`

`UserStore`, `PasswordHasher`, and `TokenIssuer` are separate ports.
`AuthService` composes them. An OIDC provider later replaces `PasswordHasher`
usage with a token-exchange flow while `UserStore` and `TokenIssuer` stay as
they are.

Alternative considered: one `Authenticator` port returning a principal.
Rejected — it hides the password/token split, so the OIDC change would rewrite
the whole port rather than add beside it.

### D3. Effective role = max(global role, project grant)

A user has one global role and zero or more per-project grants. For a
project-scoped route the effective role is the higher of the two on the ordered
scale `viewer < operator < admin`. A global `operator` therefore keeps working
on every project, and a global `viewer` can be raised to `operator` on one
project without becoming one everywhere.

Alternative considered: the grant overrides the global role in both directions,
so a grant could demote. Rejected — a demoting grant means the safe reading of
a user's row is no longer "at least this"; every audit of who can do what would
need both tables joined.

### D4. `golang-jwt/jwt/v5` rather than hand-rolled JWT

Signing is HMAC-SHA256, which is 20 lines of stdlib, but *verification* is where
JWT implementations fail: algorithm confusion (`alg: none`, RS256→HS256), claim
validation, and constant-time comparison. The repository already hand-rolls HMAC
in `webhook_verify.go`, but that verifies an opaque digest with no claims and no
algorithm field — a much smaller contract.

Trade-off: one new direct dependency in a deliberately lean `go.mod`.
Accepted — this is the code path that decides whether a request is trusted.

### D5. `golang.org/x/crypto/argon2` for passwords

argon2id with per-user random salt, parameters stored in the encoded hash string
so they can be raised later without invalidating existing rows.

Alternative considered: bcrypt from the same module — well understood, but
capped at 72 bytes and weaker against GPU attack. Both cost the same dependency.

### D6. JWT signing key is a file, provisioned like the master key

`KESTRIX_JWT_SIGNING_KEY_PATH` is read and validated before the listener opens,
reusing the shape of `credentials.FileMasterKeyProvider`: file must exist, must
not be group- or world-readable, must hold 32 raw bytes or 64 hex characters.

Alternative considered: derive the signing key from the existing master key.
Rejected — it couples token forgery to credential decryption, so rotating one
forces rotating the other, and a leaked signing key would imply a leaked
credential key.

Alternative considered: generate a random key at startup. Rejected — every
restart would log everyone out, and a multi-replica deployment could never
validate a peer's token.

### D7. Token lifetime 60 minutes, no refresh token

Short enough that a leaked token expires on its own, long enough that a working
session is not interrupted. No refresh flow, no revocation list: logout is a
client-side token drop. Deleting a user does not kill their current token — see
Risks.

### D8. UI keeps the token in memory plus `sessionStorage`

`sessionStorage` survives a page reload but not a closed tab, and is not sent
automatically like a cookie, so there is no CSRF surface. An `AuthContext`
mirrors `ColorModeContext` (`ui/src/context/`), and `client.js` reads the token
from one place so no call site can forget the header. A `401` from any call
clears the token and routes to `/login`.

Alternative considered: `httpOnly` cookie. Better against XSS token theft, but
it needs CSRF tokens on every mutating route and a same-site story for the
`docker` deployment where UI and API are served from different ports.

## Security considerations

- Login failures return one message for both "no such user" and "wrong
  password", and the handler runs the password hash comparison even when the
  user is missing, so response time does not disclose which accounts exist.
- Password hashes never leave the store layer: user API responses carry name,
  role, and timestamps only.
- The signing key and its file path never appear in errors or logs; startup
  validation reports the condition, not the contents — matching what
  `master-key-configuration` already requires.
- `RequireRole` reads the role from the verified claims, never from a header or
  query parameter the caller controls.
- The webhook route stays outside user authentication. GitLab has no Kestrix
  account, and its shared secret is already verified in `webhook_verify.go`.
- Swagger UI (`/docs/*`) stays public: it serves the schema, not data.

## Risks / Trade-offs

- **A deleted or demoted user keeps access until their token expires (≤60 min)**
  → Accepted for this change; the mitigation is the short lifetime. A revocation
  list is a follow-up, and is listed in Non-goals.
- **Locking yourself out: no accounts means no API access, and the CLI is the
  only way in** → `kestrix user create` works against the database directly, so
  it recovers a deployment with no reachable API. Documented in the deployment
  guide alongside the key-file setup.
- **Docker E2E and Compose flows break the moment auth lands** → the same commit
  adds the `kestrix user create` step to `scripts/docker-deployment-e2e.sh` and
  documents it for Compose. Task group 6 covers this.
- **100% coverage gate on `internal/web`** → middleware has many error branches
  (missing header, malformed header, bad signature, expired, unknown user,
  insufficient role). Each needs a test; they are cheap but numerous, and are
  budgeted in the task list rather than discovered late.
- **`viewer` is the default role, so a new account can read every project** →
  Accepted. Read isolation would need the project list itself filtered by grant,
  which is a larger change; noted as an open question.

## Migration Plan

1. Deploy is not backward compatible by design: after this change the API
   requires a token. Operators run `kestrix user create --role admin` once,
   against the same `DATABASE_URL`, before pointing the UI at the new build.
2. `users` and `project_members` join the idempotent `CREATE TABLE IF NOT
   EXISTS` block, so an existing database needs no manual step.
3. `KESTRIX_JWT_SIGNING_KEY_PATH` must be provisioned before start, exactly like
   `KESTRIX_MASTER_KEY_PATH`; the server refuses to start without it.
4. Rollback is the previous binary plus dropping the new environment variable.
   The two new tables can stay: nothing else reads them.

## Open Questions

- Should `kestrix serve` log a warning on startup when the `users` table is
  empty? It changes no requirement and no task — the API behaves identically
  either way — so it can be decided while implementing.
