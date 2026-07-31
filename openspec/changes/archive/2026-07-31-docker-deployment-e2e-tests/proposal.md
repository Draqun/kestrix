## Why

Kestrix deployment is exercised through Docker Compose, but the current checks do not provide a fast, repeatable end-to-end verification of the complete deployment path. A deployment can therefore fail because of image, Compose, port, container naming, database, or healthcheck issues while unit and package tests remain green. We need a focused regression test that verifies the deployed application from the outside.

## What Changes

- Add a Docker deployment E2E test flow that builds the Kestrix image and starts the production Compose stack in an isolated test configuration.
- Wait for the deployed backend health endpoint and verify the externally reachable API.
- Verify that the deployment uses the deployment container/database profile and does not depend on the local development database.
- Exercise a minimal API flow, including project/run access, and produce actionable logs on failure.
- Add a Makefile target for running the Docker deployment E2E test quickly and document its prerequisites and cleanup behavior.
- Keep the test isolated from the developer's running dev stack through dedicated names, ports, and disposable resources.

## Capabilities

### New Capabilities

- `docker-deployment-e2e`: Repeatable black-box verification of a Kestrix Docker deployment, including startup, health, API access, isolation, and cleanup.

### Modified Capabilities

- None.

## Impact

- `scripts/`, `docker/`, and the root `Makefile`.
- Docker Engine and Docker Compose during the test; the test will create and remove disposable containers, networks, and volumes.
- CI/local developer documentation.
- No production API or runtime behavior changes.

## Non-goals

- Replacing unit, integration, frontend, or pipeline-runner tests.
- Testing the separate business website.
- Deploying to a remote host or GitLab environment.
