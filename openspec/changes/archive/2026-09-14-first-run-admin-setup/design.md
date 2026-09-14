## Context

See proposal.md - Why. Today the only bootstrap is `cmdUserCreate` (`cmd/kestrix/user.go`), which talks to the store directly. The web layer already has everything a setup endpoint needs: `requireUserStore`, `passwordHasher`, `invalidateIdentity` (`internal/web/handler_user.go`), and the per-address login throttle (`internal/web/handler_auth.go`, `middleware.go`). `UserStore` (`internal/domain/ports.go:293-305`) has `CreateUser` and `ListUsers` but no atomic "create only if empty".

```
Browser ──GET /api/setup──▶ handler_setup ──ListUsers/Count──▶ UserStore
   │                                                              │
   └─POST /api/setup {name,pw}─▶ throttle ─▶ hash ─▶ CreateFirstUser (tx)
                                                        │ count==0 ? INSERT : ErrSetupDone
   ◀── 201 ── then POST /api/auth/login (existing) ◀──────┘
```

## Goals / Non-Goals

**Goals:**
- One transaction decides "empty → insert admin", so two racing requests cannot both succeed.
- Zero new configuration; reuse login throttle limits.
- UI path and CLI path produce identical accounts.

**Non-Goals:**
- A generic "setup wizard" for other settings.
- Changing how `POST /api/users` behaves.

## Decisions

- **D1: New store method `CreateFirstUser(name, role, hash)` instead of count-then-create in the handler.** The check and the insert must be in one transaction (`SELECT count(*) FROM users FOR UPDATE` is not possible on an aggregate, so use `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE` or `pg_advisory_xact_lock` then count then insert). Returns `ErrSetupDone` when count > 0. Alternative rejected: handler-level check plus `CreateUser` - a race window where two admins get created.
- **D2: Setup succeeds with `201` and the UI then calls the existing login.** Alternative: return a token from setup. Rejected: duplicates token issuance and cookie logic; one extra round-trip is invisible to the user.
- **D3: `409` after setup is done, not `404` or `403`.** The route exists and is conflict-free to advertise; `409` matches the existing "user already exists" convention in `handleCreateUser`.
- **D4: Reuse the login throttle keyed by client address only.** There is no account to key by; the per-address limiter already exists. Alternative: unthrottled because the route closes itself. Rejected: an empty instance exposed to the internet would accept password-guessing-style spam and unbounded hashing work.
- **D5: UI gate lives in `AuthContext`, not in `Login.jsx`.** `AuthContext` already fires one `GET /api/auth/me` on mount; a parallel `GET /api/setup` fits there and lets `App.jsx` route to `Setup` before any protected page.

## Security considerations

- The endpoint is open only while there are zero accounts; after that it is a constant `409` that reveals nothing but "setup is done", which `GET /api/setup` already reveals.
- Password goes through the same `PasswordHasher` as every other account; never logged.
- The success log line names the account and client address, following the auth logging convention; it never includes the password.
- No CSRF concern: the route is only useful pre-authentication, and it requires a JSON `Content-Type` like login does.

## Risks / Trade-offs

- [An operator exposes a fresh instance publicly and someone else claims the admin account first] → Documented in the install guide: run setup right after `up -d`, or pre-create the admin via CLI; throttle limits blind guessing but not a first-comer. This is the same window every self-hosted product with a setup page has.
- [`LOCK TABLE` under a long-running migration] → setup runs after `Migrate` completes; lock is held for one short transaction.

## Migration Plan

Additive. Existing deployments already have accounts, so `GET /api/setup` returns `required: false` and nothing changes for them. Rollback: remove the two routes; no schema change.
