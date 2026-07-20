# Spec: project-docs

## Purpose
The documentation system generates a versioned Markdown site that aggregates specifications for Kestrix components.

## Overview

The documentation system provides a versioned Markdown site containing architectural, feature, and usage documentation for each component of Kestrix.

## Requirements

### REQ-1: Documentation site generation (pending)
The system SHALL generate a `docs/` directory containing a Markdown file for each major component (CI generator, DAG engine, pipeline definition, webhook receiver, dispatch pipeline, project registration). The generated documentation SHALL be copied into the UI build output (`ui/dist/docs`) and be served at runtime under the path `/docs/`.

#### Scenario: UI build includes docs
- **WHEN** the `make ui-build` target runs,
- **THEN** the build process shall copy the `openspec/specs/**/spec.md` files (rendered as Markdown) into `ui/dist/docs`.