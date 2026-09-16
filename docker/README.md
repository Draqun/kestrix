# Kestrix Docker resources

The Docker Compose configuration uses `kestrix` for application,
business-site, PostgreSQL database, and test-database identities. Runtime
configuration uses only the canonical `KESTRIX_*` environment variables.

See [Production deployment](../docs/deployment.md) for the full install,
upgrade, backup, and TLS walkthrough. Summary of `docker-compose.prod.yml`:

- Plain `up -d` starts only `kestrix` and `postgres`; `POSTGRES_PASSWORD` is
  required and the database is never published on the host.
- `--profile tls` (plus `KESTRIX_DOMAIN`) adds a Caddy reverse proxy that
  terminates HTTPS with an automatic certificate.
- `--profile db-port` (plus `POSTGRES_PORT`) publishes PostgreSQL on the host
  via a small TCP relay, not Postgres's own listener (Compose can't gate one
  service's `ports` entry on a profile) - bound to `127.0.0.1` unless
  `POSTGRES_BIND_HOST` says otherwise.
- `--profile business` starts the marketing site; most installs don't need it.

## Credential master key

Both Compose profiles require a separately provisioned, mode-`0600` file with
a 32-byte raw or 64-character hexadecimal master key. Set its host path before
starting the stack:

```bash
export KESTRIX_MASTER_KEY_PATH=/srv/kestrix/secrets/master.key
docker compose -f docker/docker-compose.prod.yml up -d
```

Compose mounts this file read-only at the same path inside the container. Do
not put the key in the repository or PostgreSQL volume. Retain the same file
when upgrading: it decrypts existing repository credentials.

## Migration from the old names

Renaming the Compose resources does not automatically migrate or remove
containers or volumes created with previous names. Existing PostgreSQL data
must be migrated by an administrator before switching the connection URL.
Before switching an existing installation, stop the old stack and explicitly
recreate or migrate its resources as appropriate for the environment.

Do not run `make clean` unless removing the Compose volumes is intentional.
