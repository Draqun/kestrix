## Context

The business site is built into `ui/dist/business`, but `scripts/deploy-business.sh` treats a local deployment as a host filesystem copy to `/var/www/business`. The production Compose stack currently serves only the Kestrix application and its dependencies, so the business artifact has no Docker service or URL. The local runner executes Bash on the host and can invoke Docker Compose.

## Goals / Non-Goals

**Goals:**

- Serve the built business artifact from a dedicated Docker Compose service.
- Make `business_deploy` idempotent and runnable without privileged host filesystem access.
- Preserve remote deployment through `DEPLOY_HOST` and rsync.
- Make the local endpoint and configuration explicit and verifiable.

**Non-Goals:**

- Changing the Vite build or business website content.
- Adding TLS, authentication, CDN integration, or a production hosting provider.
- Replacing the existing application deployment task.

## Decisions

### Use nginx as a dedicated static-site service

Add a `business` service to the production Compose file using a small nginx image. Mount `../ui/dist/business` read-only at nginx's document root and expose a configurable host port (default `8082`). This avoids copying files into `/var/www`, keeps the deployment reproducible, and makes the artifact visible to the same Docker stack.

Alternatives considered: serving the site from the Go application would couple static-site concerns to the API container; copying into a named volume would require an extra container and make artifact inspection/refresh less direct.

### Make the script select local Compose or remote rsync

When `DEPLOY_HOST` is empty, the script validates the source and runs `docker compose -f ... up -d --force-recreate business`. When `DEPLOY_HOST` is set, it retains rsync behavior. Compose file, service, and port-related values are configurable through environment variables, with safe repository-local defaults.

### Keep the source mount read-only

The container must not be able to modify checked-out build output. The build step owns artifact creation; deployment only starts/recreates the serving container.

### Architecture

```text
business_build
      |
      v
ui/dist/business  --read-only bind mount-->  nginx:alpine (business)
                                                     |
                                                     v
                                               localhost:8082

DEPLOY_HOST != ""  -->  rsync artifact to remote target
```

### Security considerations

- The static service receives no application secrets and has no write access to the source mount.
- The exposed port is configurable and should be bound to localhost by the operator when the host is shared.
- Remote deployment continues to rely on the caller's SSH/rsync configuration; the script must not log credentials.

## Risks / Trade-offs

- [Risk] A missing or stale build directory produces an empty/old site. → Mitigation: fail fast when the source directory is absent and document build-before-deploy.
- [Risk] Port `8082` may already be occupied. → Mitigation: support an environment override and report the Compose error clearly.
- [Risk] Existing users expect `/var/www/business` for local deployment. → Mitigation: treat Docker Compose as the new local target and retain host copying only through the explicit remote path; document the behavior change.

## Migration Plan

1. Build the business site with `bash scripts/build-business.sh`.
2. Run `bash scripts/deploy-business.sh`; the script starts the Compose `business` service.
3. Verify the site at `http://localhost:${BUSINESS_PORT:-8082}`.
4. Roll back by stopping/removing the `business` service or reverting the Compose/script change; the generated artifact remains in the working tree.

## Open Questions

None for the local deployment MVP.
