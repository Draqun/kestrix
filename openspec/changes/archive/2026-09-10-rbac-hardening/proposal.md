## Why

`rbac-auth` closed the open-API hole, but reviewing the merged code found six
defects that let an authorization decision go stale, fail silently, or be
sidestepped.

- `internal/web/middleware.go:25` takes the global role from the JWT alone and
  never consults the user store. A deleted account, a demoted administrator,
  and an account whose password just changed keep their old access for the full
  60-minute token lifetime. Grants are re-read every request, so the two halves
  of the effective role disagree on freshness.
- `internal/web/middleware.go:70` discards the `GrantStore` error
  (`err == nil && found`). A database outage silently drops every grant, demotes
  every caller to their global role, and reports nothing.
- `internal/web/handler_pipeline.go:24` and four sibling handlers parse a
  caller-supplied `dir`, so a `viewer` reads any directory the server process
  can reach.
- Nothing throttles `POST /api/auth/login` and no failed attempt is recorded.
- Cross-project read visibility is unstated. `/api/projects`, `/api/runs`,
  `/api/dashboard`, and `/api/logs` return everything to every account, which no
  requirement permits or forbids.
- The server sends no security response headers, so the `sessionStorage` token
  chosen in `rbac-auth` design D8 has nothing behind it.

## What Changes

- **BREAKING** for issued tokens: the global role is resolved from the user
  store on every request. Tokens predating this change carry no `token_version`
  and are rejected with `401`; every session logs in again.
- Add `token_version` to `users`, carry it in the token, and bump it on role
  change, password change, and delete, so a revoked session dies at once.
- Add a bounded in-process identity cache, so the per-request lookup costs one
  round trip per account per few seconds and stale privilege never outlives it.
- Fail closed: a user-store or grant-store error yields `503`, never a silently
  reduced role.
- Confine the caller-supplied pipeline directory to the configured root; a path
  outside it is refused with `403`.
- Throttle failed logins per account and per client address, and log every
  authentication failure and authorization denial.
- State cross-project read visibility normatively, and add the security
  response headers the token storage decision depends on.

## Non-goals

- External identity providers (OIDC, SAML, LDAP), refresh tokens, and MFA.
- Per-project read isolation. D6 keeps read visibility global; a future
  `project-visibility` capability would change that.
- A durable audit log. Decisions are logged, not stored.
- Custom roles, UI permission editing, and service accounts.
- Distributed rate limiting; the throttle is per process.

## Capabilities

### Modified Capabilities

- `access-control`: role freshness, fail-closed store errors, bounded pipeline
  directory access, stated read visibility, security response headers.
- `user-authentication`: token version and revocation, login throttling, and
  authentication decision logging.

### New Capabilities

None.

## Impact

- `internal/web/middleware.go`: user-store lookup plus cache on every protected
  route. `internal/web/server.go`: security-header and throttle middleware.
- `internal/store/pgstore.go` and `pgstore_user.go`: `token_version` column and
  its bumps. `internal/domain/ports.go`: cache and clock seams.
- `internal/web/handler_pipeline.go`: 5 handlers validate `dir`.
- Every issued token stops working; the UI `401` path already handles it.
- `scripts/test.sh` holds `internal/web` and `internal/service` at 100%.
