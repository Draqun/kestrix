## Why

A fresh Kestrix instance serves no request until an administrator account exists, and the only way to create one is `kestrix user create --role admin` run inside the container (`docker/docker-compose.prod.yml:10-13`, `cmd/kestrix/user.go:90`). A customer evaluating the product from the UI hits the login screen with no account and no path forward except a shell. This is the first thing every self-hosted install runs into, so it gates every other go-to-market gap.

## What Changes

- New open route `GET /api/setup` reporting whether first-run setup is still required (`{"required": true}` while the account table is empty, `false` afterwards).
- New open route `POST /api/setup` that creates the first account with the `admin` role from `{name, password}`. It succeeds only while no account exists; once any account exists it answers `409` and never creates or modifies anything, regardless of the payload.
- Creation is atomic against concurrent setup requests: at most one succeeds, the others receive `409`.
- `POST /api/setup` is throttled per client address with the same limits as login.
- The UI asks `GET /api/setup` before showing the login form. While setup is required it renders a "Create the first administrator" form instead, and after success signs the new account in and lands on the dashboard.
- `kestrix user create` keeps working unchanged; the two paths are equivalent.
- Compose comments, README Quick Start and `docs/authentication.md` describe the UI path first and the CLI path as the scripted alternative.

## Non-goals

- Self-service sign-up after the first account (still admin-only via `/api/users`).
- Password reset or e-mail flows.
- Any change to roles, grants or token issuance.
- A setup wizard for other configuration (webhook secret, GitLab token); those stay environment/config-file settings.

## Capabilities

### New Capabilities

- `first-run-setup`: the bootstrap contract — when setup is offered, what it creates, how it closes itself, and how the UI surfaces it.

### Modified Capabilities

- `access-control`: the "Authentication required on API routes" requirement gains `GET /api/setup` and `POST /api/setup` as open routes, alongside health and login.

## Impact

- `internal/web/server.go` (route registration in the open group), new `internal/web/handler_setup.go`, reuse of `passwordHasher`, `requireUserStore`, `invalidateIdentity` and the login throttle from `handler_auth.go`/`middleware.go`.
- `internal/domain/ports.go` `UserStore` gains a way to create the first account only when none exists; `internal/store/pgstore_user.go` implements it in one transaction.
- `ui/src/api/client.js` (`setupStatus`, `completeSetup`), `ui/src/context/AuthContext.jsx` (setup-required state), new `ui/src/pages/Setup.jsx`, `ui/src/App.jsx` routing.
- `docs/authentication.md`, `README.md` Quick Start step 2, `docker/docker-compose.prod.yml` header comment, `CHANGELOG.md`, Swagger regeneration.
