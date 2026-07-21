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