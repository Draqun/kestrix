## Why

`docker/docker-compose.prod.yml` is what a customer will run, and today it ships one hard-coded Postgres password for every install (`kestrix:kestrix`, lines 20 and 91), publishes Postgres on the host (`5432`), serves the API over plain HTTP with no reverse-proxy or TLS guidance anywhere in `docker/` or `docs/`, and starts a `business` marketing-site container the customer does not want. `KESTRIX_COOKIE_SECURE` defaults to `true`, so a customer following the file as written cannot even log in from a browser over HTTP without discovering that flag. A self-hosted product needs an install that is safe by default and documented end to end.

## What Changes

- **BREAKING for existing prod deployments**: `POSTGRES_PASSWORD` becomes a required variable (`:?` guard); `DATABASE_URL` is composed from it. An operator upgrading must set it to the value already in their volume (`kestrix`) or rotate it.
- Postgres is no longer published on a host port by default (`POSTGRES_PORT` opt-in, bound to `127.0.0.1`).
- The `business` service moves behind a Compose profile (`--profile business`) so a plain `up -d` starts only Kestrix and Postgres.
- New `docker/Caddyfile` and a `proxy` service (profile `tls`) that terminates HTTPS with automatic certificates for a `KESTRIX_DOMAIN`, forwards to `kestrix:8080`, and sets `X-Forwarded-For`; Compose sets `KESTRIX_TRUST_PROXY_HEADER=true` and keeps `KESTRIX_COOKIE_SECURE=true` in that profile. Without the profile the file documents `KESTRIX_COOKIE_SECURE=false` for LAN/HTTP evaluation.
- New `docs/deployment.md`: prerequisites, key provisioning, `.env` from `.env.template`, first start, first admin, webhook exposure, upgrade (`pull` + `up -d`), backup (`pg_dump`) and restore, and rollback to a pinned image tag.
- `.env.template` gains the new variables; `scripts/docker-deployment-e2e.sh` and `scripts/deploy.sh` pass `POSTGRES_PASSWORD`.

## Non-goals

- Kubernetes manifests or Helm.
- Managed-database setups (RDS etc.) beyond "set `DATABASE_URL` yourself".
- In-app TLS termination in the Go binary.
- Automated backup scheduling; the doc gives the commands.

## Capabilities

### New Capabilities

- `production-deployment`: what the shipped production Compose deployment guarantees out of the box (no shared secrets, no unintended exposure, a documented TLS path, a documented lifecycle).

### Modified Capabilities

(none — `docker-deployment-e2e` keeps its requirements; its script gains one variable.)

## Impact

- `docker/docker-compose.prod.yml`, new `docker/Caddyfile`, `.env.template`, `scripts/deploy.sh`, `scripts/docker-deployment-e2e.sh`, `docker/README.md`.
- New `docs/deployment.md`; `README.md` Quick Start links to it; `CHANGELOG.md` (BREAKING entry).
- No Go change.
