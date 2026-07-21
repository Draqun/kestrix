## ADDED Requirements

### Requirement: Comprehensive project documentation
The system SHALL provide a complete set of documentation covering all major components of the Kestrix project, including Front‑end, Back‑end, CI tools, Terraform provider, and Business pages. This documentation shall be versioned alongside the code and be accessible from the repository's `docs/` directory.

#### Scenario: Documentation available after release
- **WHEN** a new release tag is created and the CI pipeline runs
- **THEN** the generated documentation site includes the latest Markdown files for each component and is published to the documentation server.
