# Production deployment

This guide covers installing Kestrix from `docker/docker-compose.prod.yml`: a
customer-facing, safe-by-default deployment with Kestrix and PostgreSQL, an
optional TLS reverse proxy, and no shared secrets.

## Prerequisites

- Docker Engine with the Compose plugin (`docker compose version`).
- A host with a public IP if you intend to receive webhooks or use the TLS
  profile. A LAN-only evaluation does not need one.
- For the TLS profile: a domain name whose DNS `A`/`AAAA` record points at
  this host, and ports 80/443 reachable from the internet (Let's Encrypt
  validates over port 80 before issuing a certificate).

## 1. Provision keys and secrets

Two key files, mode `0600`, generated once and kept for the life of the
deployment - losing the master key makes every stored repository credential
unrecoverable:

```bash
mkdir -p /srv/kestrix/secrets
(umask 077 && openssl rand -hex 32 > /srv/kestrix/secrets/master.key)
(umask 077 && openssl rand -hex 32 > /srv/kestrix/secrets/jwt.key)
```

Generate a database password:

```bash
openssl rand -hex 24
```

Use this command, or another generator restricted to `[0-9a-f]` - the value
is substituted verbatim into `DATABASE_URL` below and Compose cannot escape
it, so a password containing `@ : / ? # %` breaks the composed URL
(connects to the wrong host, or fails to parse) rather than refusing to
start.

## 2. Configure `.env`

```bash
cp .env.template .env
```

At minimum, set:

```bash
KESTRIX_MASTER_KEY_PATH=/srv/kestrix/secrets/master.key
KESTRIX_JWT_SIGNING_KEY_PATH=/srv/kestrix/secrets/jwt.key
POSTGRES_PASSWORD=<the value generated above>
```

Compose refuses to start without `POSTGRES_PASSWORD`: the database container
requires it, and it also composes the Kestrix `DATABASE_URL`
(`postgres://kestrix:$POSTGRES_PASSWORD@postgres:5432/kestrix`). Set
`DATABASE_URL` directly instead if you point Kestrix at a managed database;
an explicit `DATABASE_URL` always wins.

## 3. First start

```bash
cd docker
docker compose -f docker-compose.prod.yml up -d
```

This starts exactly `kestrix` and `postgres` - no marketing site, no exposed
database port. Confirm health:

```bash
curl http://localhost:18080/api/health
```

## 4. Create the first administrator

Open the UI (`http://<host>:18080`, or your TLS domain once configured): a
fresh instance shows a "Create the first administrator" form instead of
login. For a scripted install, create the account from the CLI instead -
either path produces the same account and closes the other:

```bash
docker compose -f docker-compose.prod.yml exec kestrix \
  kestrix user create --role admin <name>
```

## 5. Expose webhooks

Register a webhook in your Git host (GitLab → Settings → Webhooks) pointing
at `http://<host or domain>:<port>/webhook`, with a per-project secret (set
via `PUT /api/projects/{name}` or the Settings UI) or the global
`KESTRIX_WEBHOOK_SECRET` as the token. Verification fails closed: a delivery
matching neither is rejected with `401` and nothing is recorded. See
[Webhook receiver](webhook-receiver.md) for resolution and fan-out.

## 6. TLS profile vs. plain-HTTP evaluation

**TLS (recommended for anything beyond a LAN evaluation).** Set
`KESTRIX_DOMAIN` in `.env` to your domain, **and also set
`KESTRIX_TRUST_PROXY_HEADER=true`** - Compose cannot set this automatically
just because `KESTRIX_DOMAIN` is present, since the `kestrix` service always
starts regardless of which profile you use, and defaulting it to `true`
whenever a domain happens to be configured would let any direct client
forge its own login-throttle key before the proxy is even running. Then
start with the `tls` profile:

```bash
docker compose -f docker-compose.prod.yml --profile tls up -d
```

This starts `proxy` (Caddy), which obtains a certificate for `KESTRIX_DOMAIN`
automatically and forwards to Kestrix on port 8080, setting
`X-Forwarded-For`. With `KESTRIX_TRUST_PROXY_HEADER=true`, the login throttle
keys on the real client address; `KESTRIX_COOKIE_SECURE` stays at its default
(`true`), so the session cookie requires HTTPS - which the proxy now
provides. Never set `KESTRIX_TRUST_PROXY_HEADER=true` without a proxy
actually in front: it lets any client forge its own throttle key. Access the
UI at `https://<KESTRIX_DOMAIN>` - the plain `kestrix` port (18080) is still
reachable directly on the host network and bypasses the proxy, so firewall
it off from the internet once TLS is confirmed working.

**Plain HTTP (LAN or short-lived evaluation only).** Without the `tls`
profile, Kestrix serves plain HTTP. A browser will silently drop a `Secure`
cookie over a non-HTTPS origin, so login will appear to succeed and then
immediately look signed-out. Set in `.env`:

```bash
KESTRIX_COOKIE_SECURE=false
```

and restart (`docker compose -f docker-compose.prod.yml up -d`). Do not use
this setting on a deployment reachable from the internet: the session cookie
travels unencrypted.

## 7. Optional: expose the database

Off by default. To reach PostgreSQL from the host (a local GUI client,
`pg_dump` from outside the container), set `POSTGRES_PORT` in `.env` and
start with the `db-port` profile:

```bash
docker compose -f docker-compose.prod.yml --profile db-port up -d
```

This binds to `127.0.0.1` unless you also set `POSTGRES_BIND_HOST`. Set
`POSTGRES_PORT` whenever you use this profile - without it, the bind lands on
an unpredictable ephemeral port.

This isn't PostgreSQL's own listener published directly: Compose can't make
one service's `ports` entry conditional on a profile, so the profile starts
a small `postgres-port` TCP relay (`socat`) in front of it instead. Two
consequences: it only starts accepting connections once the `postgres`
healthcheck passes (a few seconds after `up -d`, not immediately), and it
caps concurrent connections (10) rather than deferring to PostgreSQL's own
`max_connections`.

## 8. Upgrade

```bash
docker compose -f docker-compose.prod.yml exec postgres \
  pg_dump -U kestrix kestrix > backup-$(date +%Y%m%d).sql   # see Backup below
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d
curl -f http://localhost:18080/api/health
```

Read `CHANGELOG.md` for the version range you are crossing before upgrading -
entries marked **BREAKING** may need a one-time action first (an existing
`POSTGRES_PASSWORD` you must add to `.env` before this change, for example).

## 9. Backup and restore

Backup (the database is the only state that must survive a host loss; the
master and signing keys are provisioned separately and must be backed up
alongside it - a restored database is unreadable without its original master
key):

```bash
docker compose -f docker-compose.prod.yml exec postgres \
  pg_dump -U kestrix kestrix > kestrix-backup.sql
```

Restore into a fresh volume:

```bash
docker compose -f docker-compose.prod.yml up -d postgres
docker compose -f docker-compose.prod.yml exec -T postgres \
  psql -U kestrix kestrix < kestrix-backup.sql
docker compose -f docker-compose.prod.yml up -d
```

## 10. Rollback

Pin `KESTRIX_IMAGE` to the previous release tag and restart; volumes are
untouched, so this is safe as long as no forward-only database migration ran
in between (check `CHANGELOG.md` for the versions in between):

```bash
KESTRIX_IMAGE=kestrix:<previous-tag> docker compose -f docker-compose.prod.yml up -d
```

## Non-goals

This guide covers a single-host Compose install. Kubernetes/Helm, managed
databases beyond "set `DATABASE_URL` yourself", and automated backup
scheduling are out of scope; the commands above are what you would put in
your own scheduler.
