## Why

The Docker configuration still contains the former project identity `metaci` in service names, container names, database defaults, comments, and helper commands, while the product is now named Kestrix. This creates confusing container discovery, logs, and operational instructions and makes the Docker setup inconsistent with the rest of the project.

## What Changes

- Replace Docker-facing project identity references from `metaci` with `kestrix`.
- Rename Docker Compose services and containers where the name represents the application or its supporting resources.
- Update Docker Makefile targets, commands, comments, health checks, and initialization messages to use Kestrix naming.
- Keep existing PostgreSQL database, user, password, and test-database names because they are part of the application/test configuration contract.
- Preserve the existing `METACI_*` environment-variable names and application configuration contract unless a separate migration explicitly changes them.
- Verify that development and production Compose configurations still start, resolve dependencies, and expose the same ports.

## Capabilities

### New Capabilities

- `docker-project-identity`: Docker development and production resources SHALL use the Kestrix project identity consistently.

### Modified Capabilities

None. This change updates Docker-facing identifiers and operational text; it does not change application behavior or public API requirements.

## Impact

- `docker/docker-compose.yml`
- `docker/docker-compose.prod.yml`
- `docker/Makefile`
- `docker/postgres-init.sh`
- Docker comments and related local operational references.

Existing local Docker volumes or scripts that refer to the old service/container names may need to be recreated or updated. PostgreSQL names and environment variables prefixed with `METACI_` remain unchanged for compatibility.

## Non-goals

- Renaming the Go module, package paths, or source directories.
- Renaming `METACI_*` environment variables.
- Changing ports, images, application behavior, database schema, or deployment semantics.
- Migrating existing production data or Docker volumes automatically.
