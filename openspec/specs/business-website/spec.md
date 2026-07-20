# Spec: Business Website

## Purpose
Provide a business‑facing static website built and deployed automatically as part of the CI pipeline.

## Overview
The Business Website component contains marketing pages, product information, and contact forms. It is built using the existing UI tooling (Vite) and outputs static assets that are served by a web server. The site is versioned alongside the Kestrix code and is deployed automatically after a successful pipeline run.

## Requirements

### Requirement: Build Business Website
The CI pipeline SHALL run the UI build process for the business website, generating static assets in `ui/dist/business`.

#### Scenario: Build during pipeline
- **WHEN** the pipeline reaches the `ui-build` stage,
- **THEN** the build command `npm run build -- --output-path ui/dist/business` SHALL be executed,
- **AND** the resulting static files SHALL be packaged for deployment.

### Requirement: Deploy Business Website
The CI pipeline SHALL deploy the built business website to the production environment (e.g., copy to `/var/www/business` or push to a CDN).

#### Scenario: Deploy after successful build
- **WHEN** the UI build succeeds,
- **THEN** the deployment step SHALL copy the contents of `ui/dist/business` to the target location,
- **AND** the deployment SHALL be recorded as part of the pipeline status.

### Requirement: Documentation inclusion
The business website documentation SHALL be included in the project‑docs site and shall be kept up‑to‑date with any behaviour changes.

#### Scenario: Documentation included in UI build
- **WHEN** the UI build runs,
- **THEN** the business website documentation shall be present in `ui/dist/docs`.
