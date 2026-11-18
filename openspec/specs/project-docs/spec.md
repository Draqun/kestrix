# Spec: project-docs

## Purpose
The documentation system generates a versioned Markdown site that aggregates specifications for Kestrix components.

## Overview

The documentation system provides a versioned Markdown site containing architectural, feature, and usage documentation for each component of Kestrix.

## Requirements

### REQ-1: Documentation site generation
The system SHALL generate a `docs/` directory containing a Markdown file for each major component (CI generator, DAG engine, pipeline definition, webhook receiver, dispatch pipeline, project registration). The generated documentation SHALL be copied into the UI build output (`ui/dist/docs`) and be served at runtime under the path `/docs/`.

#### Scenario: UI build includes docs
- **WHEN** the `make ui-build` target runs,
- **THEN** the build process shall copy the `openspec/specs/**/spec.md` files (rendered as Markdown) into `ui/dist/docs`.

### REQ-2: Comprehensive project documentation
The system SHALL provide a complete set of documentation covering all major components of the Kestrix project, including Front‑end, Back‑end, CI tools, Terraform provider, and Business pages. This documentation shall be versioned alongside the code and be accessible from the repository's `docs/` directory.

#### Scenario: Documentation available after release
- **WHEN** a new release tag is created and the CI pipeline runs
- **THEN** the generated documentation site includes the latest Markdown files for each component and is published to the documentation server.

### Requirement: Complete OpenAPI documentation for all API and webhook routes
The documentation system SHALL include OpenAPI (`docs/swagger.yaml` and `docs/swagger.json`) definitions for every `/api/*` and `/webhook` HTTP endpoint and method registered by the web server, and automated tests SHALL fail if any registered route is missing from the generated specification.

#### Scenario: All registered API endpoints appear in Swagger spec
- **WHEN** `docs/swagger.yaml` is loaded by the test suite
- **THEN** every `/api/*` and `/webhook` route and HTTP method registered in `internal/web/server.go` (including project run/cancel/manual-approval/definition, `/api/version`, `/api/credentials*`, `DELETE /api/runners/{id}`, and `/api/runner-jobs/*`) is present in `paths`

#### Scenario: Undocumented route fails route-parity test
- **WHEN** a new `/api/*` route is registered in `internal/web/server.go` without regenerating `docs/swagger.yaml`
- **THEN** `go test ./docs/...` fails reporting the undocumented HTTP method and path

### Requirement: Strict OpenSpec validation compliance for all canonical specs
All canonical capability specifications under `openspec/specs/*/spec.md` SHALL define a non-placeholder `## Purpose` section and concise normative requirements that pass `openspec validate --all --strict` without errors or warnings.

#### Scenario: Full specification suite passes strict validation
- **WHEN** `openspec validate --all --strict` is executed against the repository
- **THEN** every specification in `openspec/specs/` passes strict validation with zero `TBD` purpose placeholders and zero requirement-length warnings
