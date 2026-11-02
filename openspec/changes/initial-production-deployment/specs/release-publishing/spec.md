# Spec Delta: release-publishing

## ADDED Requirements

### Requirement: Published release artifacts validation
The release deployment process SHALL verify that the published container image can be pulled from the GitLab Container Registry and that the binary asset matches the release version tag.

#### Scenario: Release image smoke test
- **WHEN** release tag `v0.5.0` is published by the tag pipeline
- **THEN** `docker pull registry.gitlab.com/kestrix/kestrix:v0.5.0` succeeds
- **AND** `docker run --rm registry.gitlab.com/kestrix/kestrix:v0.5.0 version` prints `v0.5.0`
- **AND** the standalone binary asset attached to the GitLab Release executes successfully
