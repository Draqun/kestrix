# Changelog

All notable changes to Kestrix are documented here.

## [Unreleased]

### Added

- Session revocation: an account's global role, password, and token version are now resolved from the database on every request instead of trusted from the token, bounded by a short-lived in-process cache (`KESTRIX_IDENTITY_CACHE_TTL`, default `5s`, `0` disables it). Changing a role or password, or deleting an account, ends every session issued before the change - the account must log in again.
- Login throttling: `POST /api/auth/login` limits failed attempts per account name and per client address (`KESTRIX_LOGIN_MAX_ATTEMPTS`, default `10`; `KESTRIX_LOGIN_WINDOW`, default `1m`), returning `429` with `Retry-After` and skipping password verification once throttled. `KESTRIX_TRUST_PROXY_HEADER` (default `false`) opts into keying the client address from `X-Forwarded-For` behind a trusted reverse proxy.
- Security response headers on every route: `Content-Security-Policy` (overridable via `KESTRIX_CONTENT_SECURITY_POLICY`, `/docs/*` keeps its own), `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, and `X-Frame-Options: DENY`.
- Authentication and authorization decisions (failed logins, throttled attempts, `403` denials) are now logged with the account, client address, route, and reason - never the token, password, or password hash.
- Local user accounts, JWT-based login, and role-based access control (`viewer`/`operator`/`admin`, plus per-project role grants). `POST /api/auth/login` and admin-only account endpoints (`/api/users*`) are new.
- `kestrix user create|list|passwd|delete` manages accounts directly against the database, so the first administrator can be created without a reachable API or an existing account. Passwords are always read from a terminal prompt or standard input, never a flag.
- `KESTRIX_JWT_SIGNING_KEY_PATH` configures the token signing key, provisioned and validated at startup the same way as `KESTRIX_MASTER_KEY_PATH`.
- The UI requires sign-in before showing any data, attaches the session token to every request, and hides actions the signed-in role cannot perform (server-enforced regardless of what the UI shows).
- Durable webhook history: every parsed webhook event is recorded in PostgreSQL and survives a server restart, including events that are ignored, skipped, or failed. Requests rejected before their payload is parsed are not recorded.
- `KESTRIX_WEBHOOK_HISTORY_LIMIT` (default `1000`, also `webhook.history_limit` in the config file) bounds retained webhook events; the oldest are discarded once the limit is exceeded.
- `GET /api/logs` accepts `project`, `status`, `since`, and `until` filters alongside `limit` and `offset`.
- The Webhook Logs page offers project, status, and date-range filters with pagination, and shows explicit empty and error states.

### Changed

- **BREAKING**: Every token carries a per-account version now; a token issued before this upgrade carries none and is rejected with `401`, so every signed-in session must log in again once. The `users` table gains a `token_version` column, added automatically on startup - no manual migration step is required.
- A grant-store or user-store failure during authorization now responds `503`, never a silently reduced role or a `403` that blames the caller for an outage; and a caller-supplied pipeline directory (`dir` on `/api/pipeline`, `/api/pipeline/raw`, `/api/resolve`, `/api/generate`, `/api/validate`) is now confined to the configured pipeline root, answering `403` for a path outside it (including one that doesn't exist, so the check cannot be used to probe the filesystem).
- **BREAKING**: Every `/api` route except `GET /api/health` and `POST /api/auth/login` now requires a valid bearer token; an existing deployment must set `KESTRIX_JWT_SIGNING_KEY_PATH` and run `kestrix user create --role admin <name>` before upgrading, or the server starts but the UI and API are unusable. `POST /webhook` is unaffected: it keeps its own shared-secret verification.
- **BREAKING**: `GET /api/logs` returns the paginated `{items, pagination}` envelope already used by `/api/runs` and `/api/projects`, instead of a bare JSON array.
- `GET /api/health` reports the stored webhook count and the registered project count as separate values; both fields previously echoed the same in-memory counter.
- `GET /api/health` responds `503 Service Unavailable` with `"status": "degraded"` and omits a counter whose store query failed, instead of reporting a healthy zero behind a 200. The Compose healthchecks and `scripts/deploy.sh` read the HTTP status only, so a degraded runtime now actually fails them.
- A webhook whose GitLab pipeline dispatch fails is recorded with status `error` rather than `triggered`, so `GET /api/logs?status=error` finds it.

### Fixed

- The Webhook Logs page no longer falls back to fabricated sample events, and no longer calls a hard-coded `http://localhost:8080`, which broke the Docker deployment profile on port `18080`.

## [v0.2.0] - 2026-07-31

### Added

- PostgreSQL-backed runtime with separate local-development and deployment databases.
- Docker-based local development and production deployment profiles.
- Dedicated Docker Compose naming and port separation for development and deployment environments.
- Docker pipeline execution support, including image build and deployment health checks.
- Business website deployment through a dedicated Nginx container.
- Manual pipeline task approval and run-status handling in the UI.
- Additional unit, integration, and regression coverage for runtime, web, runner, and deployment behavior.

### Changed

- Kestrix is now the canonical project and binary identity throughout the runtime and documentation.
- `make docker-up` starts development dependencies only; `make docker-up-all` also starts the Kestrix development container.
- Local development uses PostgreSQL on port `5433` and Redis on port `6380`.
- Docker deployment uses PostgreSQL on port `5432`, Redis on port `6379`, and Kestrix on port `18080`.
- Frontend lint/build checks are more reliable and include a `make format` convenience target.
- Project registration and pipeline configuration no longer depend on the legacy `demo-mono` startup fixture.

### Fixed

- Deployment no longer stops the local development backend, preventing browser `NetworkError` failures during manual deployment.
- Docker Compose container-name and project collisions between local and deployed environments.
- Runtime startup and test configuration now consistently require PostgreSQL instead of the removed file store.
- Frontend lint warnings and build failures caused by stale output permissions and unused code.

### Release notes

- The previous release tag was `0.1.1`.
- Release tags use the `v` prefix from this release onward: `v0.2.0`.

## [0.1.1]

Previous release. See the repository history for its detailed changes.
