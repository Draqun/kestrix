## Why

Every `/api/*` route is served without authentication. Anyone who reaches the
port can read pipeline definitions, start and cancel runs, and create or rotate
repository credentials. Encrypted credential storage and the master-key work
only protect data at rest, so the HTTP surface is now the weakest part of the
system and blocks any deployment beyond a trusted network.

## What Changes

- Add local user accounts stored in PostgreSQL, with password hashing and a
  `POST /api/auth/login` endpoint that returns a short-lived JWT.
- Put identity behind a domain port so an external provider (GitLab OIDC) can be
  added later without touching handlers.
- **BREAKING** Require a valid token on every `/api/*` route except
  `/api/health` and `/api/auth/login`. Unauthenticated requests receive `401`.
- Add three global roles — `admin`, `operator`, `viewer` — and per-project role
  grants, and enforce them per route: reads need `viewer`, run and pipeline
  writes need `operator`, and user, credential, and project lifecycle
  administration needs `admin`.
- Add `kestrix user create` (and companion list/passwd/delete commands) as the
  only way to create the first administrator; the server never seeds accounts
  and never runs unauthenticated.
- Add a login screen to the UI, attach the token to every API call, redirect to
  login on `401`, and hide actions the current role cannot perform.
- Keep `POST /webhook` on its existing shared-secret verification; it is called
  by GitLab, not by a user.

## Non-goals

- External identity providers (OIDC, SAML, LDAP). The port is introduced now;
  no provider is implemented.
- Per-resource ACLs, custom roles, or permission editing in the UI.
- Refresh tokens, session revocation lists, multi-factor authentication, and
  password reset by email.
- Audit logging of authorization decisions, rate limiting, and account lockout.
- Service accounts and long-lived API tokens for CI consumers.

## Capabilities

### New Capabilities

- `user-authentication`: Local user accounts, password verification, JWT
  issuance and validation, and the CLI that manages accounts.
- `access-control`: Global roles, per-project grants, and their enforcement on
  every HTTP route.

### Modified Capabilities

None. No existing spec states who may call the API, so no existing requirement
changes; the new capabilities add that contract.

## Impact

- New `internal/domain` ports for the user store, password hasher, and token
  issuer; new `internal/auth` package; new `users` and `project_members` tables
  in `internal/store/pgstore.go`.
- Router-level middleware in `internal/web/server.go` covering all 28 `/api`
  routes; new `handler_auth.go` and `handler_user.go`.
- New `KESTRIX_JWT_SIGNING_KEY_PATH` runtime setting, provisioned like
  `KESTRIX_MASTER_KEY_PATH`.
- `ui/src/api/client.js`, `App.jsx`, and every page that renders a mutating
  action.
- Docker E2E and Compose flows need a `kestrix user create` step before they can
  call the API.
