## 1. Compose file

- [ ] 1.1 In `docker/docker-compose.prod.yml` add `POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?...}` to the database service and `DATABASE_URL=${DATABASE_URL:-postgres://kestrix:${POSTGRES_PASSWORD}@postgres:5432/kestrix?sslmode=disable}` to Kestrix; verify `docker compose -f docker/docker-compose.prod.yml config` fails without the variable and renders the composed URL with it.
- [ ] 1.2 Remove the default host port from the database service and make it `${POSTGRES_BIND_HOST:-127.0.0.1}:${POSTGRES_PORT}` only when `POSTGRES_PORT` is set (use a profile `db-port`); verify `config` output has no `ports` for postgres by default.
- [ ] 1.3 Put the `business` service under `profiles: [business]`; verify `docker compose ... config --services` lists only `kestrix` and `postgres` without the profile.
- [ ] 1.4 Add `docker/Caddyfile` and a `proxy` service (`caddy:2-alpine`, `profiles: [tls]`, ports 80/443, `KESTRIX_DOMAIN` required in that profile) forwarding to `kestrix:8080`; set `KESTRIX_TRUST_PROXY_HEADER` default to `true` only via the profile's environment; verify `config` renders correctly with `--profile tls` and `caddy validate` passes on the Caddyfile.

## 2. Scripts and template

- [ ] 2.1 Add `POSTGRES_PASSWORD`, `POSTGRES_PORT`, `POSTGRES_BIND_HOST`, `KESTRIX_DOMAIN` to `.env.template` with comments; update `scripts/deploy.sh` and `scripts/docker-deployment-e2e.sh` to pass a generated `POSTGRES_PASSWORD`; verify `make docker-e2e` (or the e2e script directly) passes end to end.

## 3. Docs

- [ ] 3.1 Write `docs/deployment.md` covering prerequisites, key provisioning, `.env`, first start, first admin, webhook exposure, TLS profile vs plain-HTTP evaluation (`KESTRIX_COOKIE_SECURE=false`), upgrade, backup/restore (`pg_dump`/`psql`), rollback to a pinned tag; verify `docs/docs_test.go` passes and the file is linked from `README.md` and `docker/README.md`.
- [ ] 3.2 Add the **BREAKING** `CHANGELOG.md` entry with the one-line upgrade fix for existing deployments.

## 4. Verification

- [ ] 4.1 On a clean VM or a fresh Docker context: follow `docs/deployment.md` literally with the TLS profile against a real DNS name (or the plain-HTTP section if no domain is available), log in via the browser, register a project, deliver a test webhook; record which of the two paths was actually exercised in the task note.
