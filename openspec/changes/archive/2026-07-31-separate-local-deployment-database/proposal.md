## Why

The local Kestrix process and the Docker deployment now connect to the same PostgreSQL database, so projects and run history created in one environment appear in the other. This regressed from the earlier setup: the local development compose did not provide `DATABASE_URL` and the application could fall back to file-backed local storage, while production used its own PostgreSQL database.

The regression came from two related changes: the development compose was changed to provide PostgreSQL settings, and `refactor: require PostgreSQL and remove FileStore` removed the local file-store fallback and hardcoded the local default to `localhost:5432/kestrix`. The current dev and prod compose files also share the `kestrix` database name, host port `5432`, container name `kestrix-postgres`, and volume name `postgres_data`.

## What Changes

- Give local development and Docker deployment distinct database identities and storage.
- Make the local development database URL explicit and separate from production instead of relying on the production-shaped fallback.
- Use distinct compose project/container/volume/database settings so local and deployed data cannot overlap on the same host.
- Preserve the test database guardrails and require test connections to remain on a database ending in `_test`.
- Add configuration and integration coverage proving the two runtime profiles resolve to different databases.
- Document the historical source of the regression and the supported local/deployed database setup.

## Capabilities

### New Capabilities

- `database-isolation`: Runtime profiles SHALL use separate database identities and storage for local development and deployment.

### Modified Capabilities

- None.

## Impact

- Affected `docker/docker-compose.yml`, `docker/docker-compose.prod.yml`, local environment templates, and startup configuration.
- Affected PostgreSQL volume/database naming and deployment migration instructions.
- Existing data is not automatically copied or deleted; operators must choose an explicit migration path if data needs to move between the old shared database and the new isolated databases.

## Non-goals

- Do not restore the removed FileStore as a second production persistence backend.
- Do not change application project/run schemas or API behavior.
- Do not delete existing PostgreSQL volumes automatically.
- Do not reuse the production database as a local development database.
