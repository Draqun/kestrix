## ADDED Requirements

### Requirement: Typed source-cache configuration

Kestrix SHALL provide typed settings for the source-cache directory, maximum
entry age, and maximum total bytes through canonical `KESTRIX_*` environment
names, configuration file fields, and explicit CLI flags. These settings SHALL
follow the established CLI-over-file-over-environment-over-default precedence
and SHALL be validated before the HTTP server starts.

#### Scenario: Configure a persistent cache directory

- **WHEN** an operator supplies a source-cache directory in runtime
  configuration
- **THEN** Kestrix SHALL resolve that value through normal configuration
  precedence
- **AND** SHALL create or validate the directory with service-account-only
  permissions before accepting traffic

#### Scenario: Invalid retention limit

- **WHEN** maximum age or maximum bytes is zero, negative, or malformed
- **THEN** startup SHALL fail with a non-secret configuration error

#### Scenario: Container deployment restarts

- **WHEN** the production container is restarted with its configured cache
  volume intact
- **THEN** Kestrix SHALL reuse valid Git object entries from that volume
- **AND** per-operation workspaces SHALL remain temporary and unshared
