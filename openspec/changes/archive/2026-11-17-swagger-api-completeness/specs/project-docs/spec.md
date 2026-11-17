# Spec Delta: project-docs

## ADDED Requirements

### Requirement: Complete OpenAPI documentation for all API and webhook routes
The documentation system SHALL include OpenAPI (`docs/swagger.yaml` and `docs/swagger.json`) definitions for every `/api/*` and `/webhook` HTTP endpoint and method registered by the web server, and automated tests SHALL fail if any registered route is missing from the generated specification.

#### Scenario: All registered API endpoints appear in Swagger spec
- **WHEN** `docs/swagger.yaml` is loaded by the test suite
- **THEN** every `/api/*` and `/webhook` route and HTTP method registered in `internal/web/server.go` (including project run/cancel/manual-approval/definition, `/api/version`, `/api/credentials*`, `DELETE /api/runners/{id}`, and `/api/runner-jobs/*`) is present in `paths`

#### Scenario: Undocumented route fails route-parity test
- **WHEN** a new `/api/*` route is registered in `internal/web/server.go` without regenerating `docs/swagger.yaml`
- **THEN** `go test ./docs/...` fails reporting the undocumented HTTP method and path
