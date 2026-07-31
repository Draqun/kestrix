## Why

The `business_deploy` pipeline step currently copies the built website to `/var/www/business` or an SSH target, while the local Kestrix deployment runs inside Docker Compose. This makes the step fail or deploy outside the locally running stack, so developers cannot verify the business website locally through the Docker deployment flow.

## What Changes

- Make the local `business_deploy` path deploy the static business site into a Docker-served component.
- Add the required Compose/web-server wiring so the deployed site is reachable locally and has a persistent, explicit deployment target.
- Keep remote deployment support available through the existing host/rsync path.
- Add tests or executable checks covering build output, local Docker deployment, and the expected served content.
- Document the local command, target, and relevant environment overrides.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `business-website`: The business website SHALL be deployable and reachable through the local Docker Compose environment, while retaining the existing remote deployment option.

## Impact

- `scripts/deploy-business.sh` and likely related build/deployment scripts.
- `docker/docker-compose.prod.yml` and web-server/container configuration for serving static business assets.
- Business website documentation and deployment checks.
- Local Docker resources (containers, networks, and volumes); no public API contract changes are expected.

## Non-goals

- Changing the business website’s pages, design, or frontend build tooling.
- Replacing the existing remote SSH/rsync deployment workflow.
- Introducing a production CDN or a new hosting provider.
