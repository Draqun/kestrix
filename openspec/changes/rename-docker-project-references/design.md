## Context

The Docker directory still mixes the current Kestrix identity with the former `metaci` identity. Compose service names, container names, database defaults, Makefile commands, and initialization messages are operational identifiers, so the mismatch is visible when starting services, reading logs, or running helper targets.

The application still consumes `METACI_*` environment variables and the repository may contain existing Docker volumes created under the old names. The change therefore needs a clear boundary between Docker-local naming and the application configuration contract.

## Goals / Non-Goals

**Goals:**

- Make Docker-local application, service, container, and operational text consistently use `kestrix`.
- Keep development and production Compose dependency graphs and ports unchanged.
- Keep `METACI_*` environment variables stable.
- Make helper targets address the renamed services.

**Non-Goals:**

- Renaming Go packages, module paths, environment variables, or database schema identifiers outside Docker configuration.
- Automatically migrating existing containers, volumes, or databases.
- Changing runtime behavior, exposed ports, images, or deployment topology.

## Decisions

### Use `kestrix` for Docker-local identifiers

Rename the main application service and its container, supporting container names, and helper command targets where the old name is a project identity. Keep PostgreSQL database/user/test-database defaults and technical variable names such as `METACI_WEBHOOK_SECRET` unchanged because they are part of the existing application and test configuration contracts.

### Update both Compose environments

Apply the same naming policy to `docker/docker-compose.yml` and `docker/docker-compose.prod.yml`. Service-to-service references must use Compose service names after the rename; published host ports remain unchanged.

### Keep migration explicit

Do not silently rename or delete existing Docker volumes. Document that operators may need to recreate or migrate resources whose names were derived from `metaci`. This avoids accidental data loss.

```text
docker compose / Makefile
          |
          v
     kestrix service
       /       \
      v         v
  postgres     redis
      |
      v
  metaci database defaults (compatibility)
```

### Verification

Validate both Compose files, search the Docker directory for unintended project-identity occurrences, and exercise Makefile command references without requiring a running production deployment. Existing `METACI_*` configuration names are explicitly allowed by the verification.

## Risks / Trade-offs

- [Risk] Existing containers or volumes retain old names and are not addressed by new commands. → Mitigation: document explicit recreation/migration and do not perform destructive cleanup automatically.
- [Risk] A service reference is renamed in one Compose file but not another. → Mitigation: update and validate development and production files together.
- [Risk] Replacing `METACI_*` names would break users' configuration. → Mitigation: treat those names as compatibility-sensitive and exclude them from this change.

## Security considerations

The change does not introduce credentials or alter secret values. Existing secret environment variables remain parameterized and are not hardcoded or renamed. Any Docker resource migration must be performed explicitly by the operator so data is not accidentally removed.

## Migration Plan

1. Update Docker-local identifiers and helper commands.
2. Validate Compose configuration and helper command references.
3. For an existing installation, stop old resources and explicitly recreate or migrate volumes as appropriate.
4. Roll back by reverting the Docker identifier changes if an external script still depends on an old service name.

## Open Questions

None for this rename-only change.
