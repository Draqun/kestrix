# Proposal: initial-production-deployment

## Why

Task 4.1 of `release-publishing` and the initial real production deployment of Kestrix were intentionally deferred until an operational target host was designated. Currently, Kestrix is verified only via synthetic test suites, local containers, and Docker deployment scripts (`docker-deployment-e2e.sh`). To transition Kestrix from development to production readiness, the project requires an official release tag (`v0.5.0`), verified public registry artifacts, an instantiated production host environment, and end-to-end verification of runner execution outside the control plane.

## What Changes

- Execute release cut for `v0.5.0` using `scripts/release.sh 0.5.0`, triggering the GitLab tag pipeline to build and publish versioned container images and release assets.
- Deploy the production stack via `docker/docker-compose.prod.yml` to a target production environment using pinned `v0.5.0` images.
- Validate security hardening: ensure PostgreSQL credentials are required, host port exposure is restricted to loopback/reverse proxy, and secrets encryption keys are stored exclusively in the server container.
- Bootstrap the primary administrator account via the first-run setup flow (`/api/setup`).
- Register a dedicated remote runner (`--profile runner`) on the production network, verifying that task execution runs isolated from keys and databases.
- Run a live verification pipeline to confirm health checks (`GET /api/health`), webhook ingestion, and artifact handling.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `production-deployment`: validates real host deployment, TLS reverse proxy wiring, and runner isolation against the published registry image.
- `release-publishing`: validates published `v0.5.0` container image and binary release assets from GitLab Container Registry and Releases.

## Non-goals

- Implementing multi-node Kubernetes clustering or Helm charts (single-node Docker Compose is the target deployment model).
- Setting up external managed databases (RDS/Cloud SQL) in Phase 1.
- Public domain DNS provisioning or automated Let's Encrypt renewal setup (provided by operator or external proxy).

## Impact

- Publishes official version `v0.5.0` to GitLab Container Registry (`registry.gitlab.com/kestrix/kestrix:v0.5.0`) and Releases.
- Establishes a verified, running Kestrix production instance with an operational remote runner.
- Closes the deferred Task 4.1 from the `release-publishing` roadmap.
