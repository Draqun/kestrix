## MODIFIED Requirements

### Requirement: Deploy Business Website

The CI pipeline SHALL deploy the built business website to the production environment. For a local deployment without `DEPLOY_HOST`, it SHALL serve the contents of `ui/dist/business` from a Docker Compose service and expose that service on a configurable local host port. For a deployment with `DEPLOY_HOST`, it SHALL retain the remote rsync deployment behavior.

#### Scenario: Deploy after successful build locally

- **WHEN** the UI build succeeds and `DEPLOY_HOST` is empty
- **THEN** the deployment step SHALL start or recreate the Docker Compose business-site service using `ui/dist/business` as its static document root
- **AND** the business website SHALL be reachable at `http://localhost:${BUSINESS_PORT:-8082}`
- **AND** the deployment SHALL be recorded as part of the pipeline status

#### Scenario: Missing local build artifact

- **WHEN** `DEPLOY_HOST` is empty and `ui/dist/business` does not exist
- **THEN** the deployment step SHALL fail with an actionable message instructing the user to run the business build first
- **AND** it SHALL NOT start a business-site container

#### Scenario: Deploy to remote host

- **WHEN** the UI build succeeds and `DEPLOY_HOST` is set
- **THEN** the deployment step SHALL copy the contents of `ui/dist/business` to the configured remote target using rsync
- **AND** it SHALL NOT require the local business-site container
- **AND** the deployment SHALL be recorded as part of the pipeline status

### Requirement: Documentation inclusion

The business website documentation SHALL be included in the project‑docs site and shall be kept up‑to‑date with any behaviour changes.

#### Scenario: Documentation included in UI build

- **WHEN** the UI build runs
- **THEN** the business website documentation shall be present in `ui/dist/docs`.
