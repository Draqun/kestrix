## MODIFIED Requirements

### Requirement: REQ-2: Comprehensive project documentation

The system SHALL provide complete documentation covering all major components of the Kestrix project, including Front‑end, Back‑end, CI tools, Terraform provider, and Business pages. This documentation SHALL be versioned alongside the code and accessible from the repository’s `docs/` directory. Repository links in active documentation SHALL use the optional source URL from `project.yaml` when configured, or repository-relative links when it is empty; they SHALL NOT hardcode a provider, owner, or obsolete repository name.

#### Scenario: Documentation uses configured source location

- **WHEN** `project.yaml` contains a source URL
- **THEN** generated or maintained documentation links to the configured source location
- **AND** the link does not require changing Go module imports

#### Scenario: Documentation remains portable without source location

- **WHEN** `project.yaml` contains an empty source URL
- **THEN** documentation SHALL still identify Kestrix and link to repository-relative paths
- **AND** it SHALL not render a stale `gitlab.com/lunaris-bot/metaci` URL
