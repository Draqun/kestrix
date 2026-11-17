# Proposal: swagger-api-completeness

## Why

Fifteen HTTP endpoints registered on the Chi router in `internal/web/server.go` currently lack `@Router` swaggo godoc annotations and are absent from `docs/swagger.yaml`, `docs/swagger.json`, and the interactive `/docs/` Swagger UI:

- `POST /api/projects/{name}/run` (`handleRunProject` in `handler_run.go`)
- `POST /api/projects/{name}/runs/{runID}/cancel` (`handleCancelRun` in `handler_run.go`)
- `POST /api/projects/{name}/runs/{runID}/tasks/{task}/run` (`handleApproveManualTask` in `handler_run.go`)
- `GET /api/projects/{name}/definition` (`handleGetProjectDefinition` in `handler_project.go`)
- `GET /api/version` (`handleVersion` in `handler_system.go`)
- `GET /api/credentials`, `POST /api/credentials`, `GET /api/credentials/{ref}`, `PUT /api/credentials/{ref}`, `POST /api/credentials/{ref}/revoke` (`handler_credentials.go`)
- `DELETE /api/runners/{id}` (`handleRevokeRunner` in `handler_runner_jobs.go`)
- `GET /api/runner-jobs/next`, `PATCH /api/runner-jobs/{id}/trace`, `PUT /api/runner-jobs/{id}/complete`, `PUT /api/runner-jobs/{id}/artifacts` (`handler_runner_jobs.go`)

Because there is no automated route-parity check in CI, newly added handlers silently drift out of the OpenAPI specification.

## What Changes

- Add complete swaggo godoc annotations (`@Summary`, `@Description`, `@Tags`, `@Accept`, `@Produce`, `@Param`, `@Success`, `@Failure`, `@Router`) to all 15 undocumented HTTP handlers in `internal/web/`.
- Regenerate `docs/swagger.yaml`, `docs/swagger.json`, and `docs/docs.go` via `swag init`.
- Add an automated route-parity unit test in `docs/docs_test.go` that inspects registered `/api/*` and `/webhook` routes in `internal/web/server.go` and fails if any route + HTTP method pair is missing from `docs/swagger.yaml`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `project-docs`: adds OpenAPI route-parity enforcement ensuring every registered `/api/*` and `/webhook` endpoint is documented in `docs/swagger.yaml` and `docs/swagger.json`.

## Non-goals

- Migrating from Swagger 2.0 (`swaggo/swag`) to OpenAPI 3.1 tooling.
- Documenting static asset catch-all routes (`/`, `/docs/*`, `/metrics`, `/healthz`).
- Changing runtime request/response schemas or authentication behavior of any existing endpoint.

## Impact

- Modifies godoc comments in `internal/web/handler_run.go`, `internal/web/handler_project.go`, `internal/web/handler_system.go`, `internal/web/handler_credentials.go`, and `internal/web/handler_runner_jobs.go`.
- Updates generated `docs/swagger.yaml`, `docs/swagger.json`, and `docs/docs.go`.
- Adds route-parity test in `docs/docs_test.go`.
