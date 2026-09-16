## 1. Compose file

- [x] 1.1 In `docker/docker-compose.prod.yml` add `POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?...}` to the database service and `DATABASE_URL=${DATABASE_URL:-postgres://kestrix:${POSTGRES_PASSWORD}@postgres:5432/kestrix?sslmode=disable}` to Kestrix; verify `docker compose -f docker/docker-compose.prod.yml config` fails without the variable and renders the composed URL with it.
- [x] 1.2 Remove the default host port from the database service and make it `${POSTGRES_BIND_HOST:-127.0.0.1}:${POSTGRES_PORT}` only when `POSTGRES_PORT` is set (use a profile `db-port`); verify `config` output has no `ports` for postgres by default.
- [x] 1.3 Put the `business` service under `profiles: [business]`; verify `docker compose ... config --services` lists only `kestrix` and `postgres` without the profile.
- [x] 1.4 Add `docker/Caddyfile` and a `proxy` service (`caddy:2-alpine`, `profiles: [tls]`, ports 80/443, `KESTRIX_DOMAIN` required in that profile) forwarding to `kestrix:8080`; set `KESTRIX_TRUST_PROXY_HEADER` default to `true` only via the profile's environment; verify `config` renders correctly with `--profile tls` and `caddy validate` passes on the Caddyfile.

## 2. Scripts and template

- [x] 2.1 Add `POSTGRES_PASSWORD`, `POSTGRES_PORT`, `POSTGRES_BIND_HOST`, `KESTRIX_DOMAIN` to `.env.template` with comments; update `scripts/deploy.sh` and `scripts/docker-deployment-e2e.sh` to pass a generated `POSTGRES_PASSWORD`; verify `make docker-e2e` (or the e2e script directly) passes end to end.

## 3. Docs

- [x] 3.1 Write `docs/deployment.md` covering prerequisites, key provisioning, `.env`, first start, first admin, webhook exposure, TLS profile vs plain-HTTP evaluation (`KESTRIX_COOKIE_SECURE=false`), upgrade, backup/restore (`pg_dump`/`psql`), rollback to a pinned tag; verify `docs/docs_test.go` passes and the file is linked from `README.md` and `docker/README.md`.

  Note: `docs/docs_test.go` contains exactly one test (`TestSwaggerInfo`),
  which only asserts Swagger metadata and has no relationship to
  `docs/deployment.md`'s content - it verifies the `docs` package still
  builds and nothing about the new guide's accuracy. The actual
  verification of `docs/deployment.md`'s commands is task 4.1's manual
  walkthrough, which followed the guide's steps literally.
- [x] 3.2 Add the **BREAKING** `CHANGELOG.md` entry with the one-line upgrade fix for existing deployments.

## 4. Verification

- [x] 4.1 On a clean VM or a fresh Docker context: follow `docs/deployment.md` literally with the TLS profile against a real DNS name (or the plain-HTTP section if no domain is available), log in via the browser, register a project, deliver a test webhook; record which of the two paths was actually exercised in the task note.

  Exercised the **plain-HTTP path** (no real DNS name/public IP available in
  this sandboxed environment for the TLS profile's Let's Encrypt validation).
  Fresh Compose project, generated keys and `POSTGRES_PASSWORD` per the guide,
  default `up -d` confirmed to start only `kestrix`+`postgres` with no host
  port for Postgres. Created the first administrator via the CLI, then
  logged in through an actual Chrome session at `http://127.0.0.1:18090/` -
  dashboard, Projects, and Webhook Logs all rendered correctly. Registered a
  project against a local bare git remote by inserting directly into
  PostgreSQL (the same technique `scripts/docker-deployment-e2e.sh` already
  uses, since `repo_url` validation rejects loopback hosts regardless of
  scheme - unrelated to this change). Delivered a webhook: unauthenticated
  request rejected `401`, authenticated request with the project's own
  secret accepted `200`, both visible in the Webhook Logs page; the project
  detail page's Webhook setup panel and "Secret set" badge rendered
  correctly. Deployment torn down and image/temp files removed afterward.
