# Changelog

All notable changes to Kestrix are documented here.

## [Unreleased]

### Added

- Run attribution: runs now record `started_by` and `canceled_by`, and a manual task's result entry records `approved_by`/`approved_at`, as the `user:<account name>` that acted. Kestrix logs one line per start, approval, and cancellation naming the actor, project, run id, and (for an approval) the task name. The two new `runs` columns are added automatically at startup by the existing idempotent schema statements - no manual migration step is needed. A run recorded before this change shows `unknown` in the UI.
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
- HTTP listener timeouts (`KESTRIX_HTTP_READ_TIMEOUT`, `KESTRIX_HTTP_WRITE_TIMEOUT`, `KESTRIX_HTTP_IDLE_TIMEOUT`; defaults `30s`/`120s`/`120s`) and per-route request-body bounds (`KESTRIX_HTTP_MAX_BODY_BYTES` for `/api`, default `1048576`; `KESTRIX_WEBHOOK_MAX_BODY_BYTES` for `POST /webhook`, default `8388608`). A request over its bound answers `413` without running the route's business logic; each setting must be a positive value or startup fails with an error naming it.
- Each project's `webhook_secret` is now a real, verified control: `PUT /api/projects/{name}` can set a per-project secret checked against every delivery for that project, alongside the existing global `KESTRIX_WEBHOOK_SECRET`.
- `repo_url` scheme and host restrictions: only `https://`, `ssh://`, and scp-like `user@host:path` are accepted; a literal loopback, link-local, or unspecified host is rejected, all with `400` before any network access.
- Run-time secrets: a new `secret` credential type holds one opaque value, created and managed through the same `/api/credentials` endpoints, `admin` role, master key, and Settings UI as Git credentials. A pipeline `variable` block can set `secret_ref` to reference one instead of a literal `value` or `default` - `secret_ref` cannot be combined with either, and a `secret_ref` variable is always masked regardless of the `masked` attribute. Every `secret_ref` is resolved once, when a run or an approved manual task starts; a missing, revoked, or non-`secret` reference fails the run before any task starts, naming only the variable, and records every task `skipped`. Resolved values of at least 8 bytes are redacted (`***`) from run and task output before it is persisted, flushed, or returned by any API; a `secret` credential is refused as a repository credential and vice versa, in both directions.

### Changed

- **BREAKING**: Every token carries a per-account version now; a token issued before this upgrade carries none and is rejected with `401`, so every signed-in session must log in again once. The `users` table gains a `token_version` column, added automatically on startup - no manual migration step is required.
- A grant-store or user-store failure during authorization now responds `503`, never a silently reduced role or a `403` that blames the caller for an outage; and a caller-supplied pipeline directory (`dir` on `/api/pipeline`, `/api/pipeline/raw`, `/api/resolve`, `/api/generate`, `/api/validate`) is now confined to the configured pipeline root, answering `403` for a path outside it (including one that doesn't exist, so the check cannot be used to probe the filesystem).
- **BREAKING**: Every `/api` route except `GET /api/health` and `POST /api/auth/login` now requires a valid bearer token; an existing deployment must set `KESTRIX_JWT_SIGNING_KEY_PATH` and run `kestrix user create --role admin <name>` before upgrading, or the server starts but the UI and API are unusable. `POST /webhook` is unaffected: it keeps its own shared-secret verification.
- **BREAKING**: `POST /webhook` verification now fails closed. A delivery is accepted only if it verifies against the matched project's own `webhook_secret` or the global `KESTRIX_WEBHOOK_SECRET`; a delivery that verifies against neither - including when neither is configured at all - is rejected with `401` and is never recorded in webhook history. The Compose files' `KESTRIX_WEBHOOK_SECRET:-dev-secret` default is removed: an existing deployment with no secret set stops accepting deliveries until `KESTRIX_WEBHOOK_SECRET` or a per-project secret is configured. Secret comparison is constant time; the previous `!=` comparison and the `KESTRIX_WEBHOOK_METHOD=hmac` setting (previously accepted by config but never honored) are fixed.
- **BREAKING**: `webhook_secret` is now write-only on the project API: `GET /api/projects`, `GET /api/projects/{name}`, `POST /api/projects`, and `PUT /api/projects/{name}` no longer return it in any form, and each response instead carries `has_webhook_secret`. An update that omits `webhook_secret` (or sends it empty) now keeps the stored secret unchanged, instead of silently clearing it - the previous behavior meant every UI save wiped a secret set via the API, since the UI never sent the field.
- **BREAKING**: `GET /api/logs` returns the paginated `{items, pagination}` envelope already used by `/api/runs` and `/api/projects`, instead of a bare JSON array.
- `GET /api/health` reports the stored webhook count and the registered project count as separate values; both fields previously echoed the same in-memory counter.
- `GET /api/health` responds `503 Service Unavailable` with `"status": "degraded"` and omits a counter whose store query failed, instead of reporting a healthy zero behind a 200. The Compose healthchecks and `scripts/deploy.sh` read the HTTP status only, so a degraded runtime now actually fails them.
- A webhook whose GitLab pipeline dispatch fails is recorded with status `error` rather than `triggered`, so `GET /api/logs?status=error` finds it.
- **BREAKING**: task processes in every runner mode (`local`, `docker`, `gitlab-runner`) now receive an explicit environment allowlist instead of the full Kestrix process environment: a fixed base set (`PATH`, `HOME`, `LANG`, `LC_ALL`, `TMPDIR`, `TERM`, `USER`), the `CI_*` variables Kestrix sets, the pipeline's own resolved variables, and only host variables named in the new `KESTRIX_RUNNER_ENV_PASSTHROUGH`. `KESTRIX_MASTER_KEY_PATH`, `KESTRIX_JWT_SIGNING_KEY_PATH`, `DATABASE_URL`, `KESTRIX_GITLAB_TOKEN`, and `KESTRIX_WEBHOOK_SECRET` can never be passed through - listing one refuses to start. A pipeline that relied on an inherited host variable must list it; this repository's own dogfood deployment sets `KESTRIX_RUNNER_ENV_PASSTHROUGH=DOCKER_REGISTRY,DOCKER_IMAGE,DOCKER_USERNAME,DOCKER_PASSWORD,DEPLOY_HOST,KESTRIX_TEST_DATABASE_URL`.
- `docker` runner mode now runs every task container with `--cap-drop ALL`, `--security-opt no-new-privileges`, a PID limit (`KESTRIX_RUNNER_DOCKER_PIDS`, default `256`), a memory limit (`KESTRIX_RUNNER_DOCKER_MEMORY`, default `2g`), and a CPU limit (`KESTRIX_RUNNER_DOCKER_CPUS`, default `2`); network is configurable via `KESTRIX_RUNNER_DOCKER_NETWORK` (default `bridge`, `none` disables egress) and an optional non-root user via `KESTRIX_RUNNER_DOCKER_USER`. The Docker socket is never mounted and `--privileged` is never passed.
- Cancelling a run now terminates its executing task before the run is recorded as `canceled`: `local` and `gitlab-runner` mode send the task's process group SIGTERM then SIGKILL, and `docker` mode stops the container, after `KESTRIX_RUNNER_CANCEL_GRACE` (default `10s`). A run whose task cannot be confirmed terminated within grace plus 5s stays `running` and the cancel request fails with `504`, instead of falsely reporting `canceled`.

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
