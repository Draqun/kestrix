# Business Website Documentation

This document covers the business-facing website and its local deployment.

## Build and deploy locally

Build the static site and deploy it to the Docker Compose nginx service:

```bash
bash scripts/build-business.sh
bash scripts/deploy-business.sh
```

The site is served at <http://localhost:8082>. The deployment mounts
`ui/dist/business` read-only into the `business` container, so it does not
require write access to `/var/www` on the host.

To stop and remove the site container:

```bash
docker compose -f docker/docker-compose.prod.yml stop business
docker compose -f docker/docker-compose.prod.yml rm -f business
```

### Overrides

- `BUSINESS_PORT` changes the local host port (default `8082`).
- `BUSINESS_BIND_HOST` changes the host bind address (default `127.0.0.1`).
- `BUSINESS_SOURCE` changes the build output directory.
- `BUSINESS_COMPOSE_FILE` selects another Compose file.
- `BUSINESS_SERVICE` selects another Compose service name.

For remote deployment, set `DEPLOY_HOST` and optionally `BUSINESS_TARGET`:

```bash
DEPLOY_HOST=web.example.test BUSINESS_TARGET=/var/www/business \
  bash scripts/deploy-business.sh
```

The remote path uses rsync and does not require the local business container.
