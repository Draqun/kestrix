# release-publishing Specification

## Purpose

Defines what pushing a version tag publishes, so a customer can install a named version of Kestrix from a public registry and report which one they run.

## Requirements

### Requirement: A version tag publishes a container image

Pushing a Git tag matching `v<major>.<minor>.<patch>` SHALL cause CI to build the production image and push it to the project's GitLab Container Registry under that exact tag and under `latest`. The pushed image SHALL report the tag as its version. A push to a branch without a tag SHALL NOT publish an image.

#### Scenario: Tag is pushed
- **WHEN** `v0.3.0` is pushed
- **THEN** `registry.gitlab.com/kestrix/kestrix:v0.3.0` and `:latest` exist and `docker run <image> version` prints `v0.3.0`

#### Scenario: Branch push
- **WHEN** a commit is pushed to `main` without a tag
- **THEN** no image is pushed to the registry

#### Scenario: Release job depends on a green pipeline
- **WHEN** any test or analysis job fails for the tagged commit
- **THEN** the release job does not run and nothing is published

### Requirement: A version tag creates a GitLab Release

The release job SHALL create a GitLab Release for the tag whose description is the matching `CHANGELOG.md` section and which links the stripped Linux amd64 binary as an asset.

#### Scenario: Release page
- **WHEN** `v0.3.0` is published
- **THEN** the project's Releases page shows `v0.3.0` with the changelog text and a downloadable `kestrix` binary

### Requirement: Production compose installs from the registry

The production Compose file SHALL default `KESTRIX_IMAGE` to the registry `latest` image, and documentation SHALL show how to pin a specific tag.

#### Scenario: Fresh install without overrides
- **WHEN** an operator runs the production Compose file without setting `KESTRIX_IMAGE`
- **THEN** Compose pulls the published `latest` image instead of failing on a missing local image

### Requirement: Changelog is cut per release

Each release SHALL move the `[Unreleased]` changelog content under a heading naming the version and date, leaving an empty `[Unreleased]` section. A release helper SHALL refuse to run on a dirty working copy or off `main`.

#### Scenario: Release helper on a dirty tree
- **WHEN** the release helper is run with uncommitted changes
- **THEN** it exits non-zero without tagging
