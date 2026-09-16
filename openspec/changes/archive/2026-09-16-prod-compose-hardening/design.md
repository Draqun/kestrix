## Context

See proposal.md - Why. The current file (`docker/docker-compose.prod.yml`) already guards the two key paths with `:?`, so the pattern exists. `KESTRIX_TRUST_PROXY_HEADER` and `KESTRIX_COOKIE_SECURE` already exist in `internal/config/environment.go` and are passed through the compose environment. `scripts/docker-deployment-e2e.sh` brings the prod file up in isolation, so it is the regression test for this change.

```
Internet ──443──▶ proxy (caddy, profile tls) ──8080──▶ kestrix ──5432──▶ postgres
                  KESTRIX_DOMAIN, auto-cert          TRUST_PROXY_HEADER=true   POSTGRES_PASSWORD (required)
                                                                              no host port by default
                  business (profile business) ─ marketing site only
```

## Goals / Non-Goals

**Goals:**
- Safe defaults with no new Go code.
- One guide a customer can follow start to finish.

**Non-Goals:**
- Supporting every proxy; one working example (Caddy) is enough. nginx users can translate.

## Decisions

- **D1: Caddy over nginx/Traefik for the TLS profile.** Automatic Let's Encrypt with a four-line Caddyfile; nginx would need certbot wiring. Alternative Traefik: labels-based config is more to explain.
- **D2: Compose profiles for `tls` and `business` instead of separate files.** One file to maintain; `docker compose --profile tls up -d` is discoverable. Alternative override files: more files, easier to run the wrong combination.
- **D3: Require `POSTGRES_PASSWORD` rather than generate one.** Compose cannot generate secrets; a `:?` guard is the honest option. The guide shows `openssl rand -hex 24`.
- **D4: Keep `DATABASE_URL` overridable.** If set explicitly, it wins, so a managed database works; otherwise it is composed from `POSTGRES_PASSWORD`.
- **D5: `sslmode=disable` stays for the in-network Postgres.** Traffic never leaves the Compose network; documenting `sslmode=require` for external databases.

## Security considerations

- Removes the one credential every install shared.
- Database no longer reachable from the host network by default.
- TLS profile keeps the `HttpOnly; Secure` cookie and enables `X-Forwarded-For` trust only when the proxy is in front; the guide warns never to set `KESTRIX_TRUST_PROXY_HEADER=true` without a proxy.

## Risks / Trade-offs

- [Existing deployment breaks on upgrade because `POSTGRES_PASSWORD` is now required] → CHANGELOG BREAKING entry with the one-line fix (`POSTGRES_PASSWORD=kestrix` to keep the old volume working, then rotate with `ALTER USER`).
- [Caddy cannot obtain a certificate (port 80 blocked, DNS wrong)] → Guide lists the two prerequisites and the `docker compose logs proxy` check.

## Migration Plan

1. Merge; existing operators add `POSTGRES_PASSWORD` to `.env`.
2. `scripts/docker-deployment-e2e.sh` runs in CI with a generated password.
Rollback: previous compose file from the prior tag; volumes are untouched.
