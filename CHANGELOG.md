# Changelog

All notable changes to Kestrix are documented here.

## [v0.2.0] - 2026-07-31

### Added

- PostgreSQL-backed runtime with separate local-development and deployment databases.
- Docker-based local development and production deployment profiles.
- Dedicated Docker Compose naming and port separation for development and deployment environments.
- Docker pipeline execution support, including image build and deployment health checks.
- Business website deployment through a dedicated Nginx container.
- Manual pipeline task approval and run-status handling in the UI.
- Additional unit, integration, and regression coverage for runtime, web, runner, and deployment behavior.

### Changed

- Kestrix is now the canonical project and binary identity throughout the runtime and documentation.
- `make docker-up` starts development dependencies only; `make docker-up-all` also starts the Kestrix development container.
- Local development uses PostgreSQL on port `5433` and Redis on port `6380`.
- Docker deployment uses PostgreSQL on port `5432`, Redis on port `6379`, and Kestrix on port `18080`.
- Frontend lint/build checks are more reliable and include a `make format` convenience target.
- Project registration and pipeline configuration no longer depend on the legacy `demo-mono` startup fixture.

### Fixed

- Deployment no longer stops the local development backend, preventing browser `NetworkError` failures during manual deployment.
- Docker Compose container-name and project collisions between local and deployed environments.
- Runtime startup and test configuration now consistently require PostgreSQL instead of the removed file store.
- Frontend lint warnings and build failures caused by stale output permissions and unused code.

### Release notes

- The previous release tag was `0.1.1`.
- Release tags use the `v` prefix from this release onward: `v0.2.0`.

## [0.1.1]

Previous release. See the repository history for its detailed changes.
