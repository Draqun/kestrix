## ADDED Requirements

### Requirement: Stable project and module identity

The repository SHALL define `Kestrix` as its product name and `kestrix` as its canonical Go module path. Active Go imports, linker flags, coverage tooling, and generated build metadata SHALL derive from or match that canonical identity and SHALL NOT depend on a Git provider, owner, or repository name.

#### Scenario: Internal model import is provider-neutral

- **WHEN** Go code imports the model package
- **THEN** the import path SHALL use `kestrix/internal/model`
- **AND** it SHALL NOT use a provider-owned URL

#### Scenario: Build tooling follows the module identity

- **WHEN** a developer runs build, version, or coverage tooling
- **THEN** the tooling SHALL resolve the module identity from `go.mod` or `go list -m`
- **AND** it SHALL not contain the former `gitlab.com/lunaris-bot/metaci` module prefix as an active constant

### Requirement: Repository source location is replaceable metadata

The repository SHALL provide a committed `project.yaml` containing `name: Kestrix` and `module: kestrix`, plus an optional source URL. Changing the source URL SHALL NOT require changing Go imports or package identity. The file SHALL NOT contain credentials.

#### Scenario: Source location changes provider

- **WHEN** the project moves to another Git provider, owner, or repository name
- **THEN** the source URL MAY be updated in `project.yaml`
- **AND** Go module imports SHALL remain unchanged

#### Scenario: Source URL is not configured

- **WHEN** `project.yaml` has an empty source URL
- **THEN** local documentation and tooling SHALL continue to identify code using repository-relative paths
- **AND** no provider-specific fallback URL SHALL be invented

### Requirement: External image locations are configurable

Container build, push, and deployment scripts SHALL NOT use a provider-owned image name as their default identity. They SHALL accept an explicit image/registry configuration through existing environment variables or fail with an actionable message when a remote image is required.

#### Scenario: Local image build without registry ownership

- **WHEN** a developer builds the container without registry variables
- **THEN** the image SHALL use a neutral local Kestrix tag or a required explicit `DOCKER_IMAGE`
- **AND** the build SHALL not assume `registry.gitlab.com/lunaris-bot/metaci`

#### Scenario: Remote publish uses explicit configuration

- **WHEN** a developer pushes or remotely deploys an image
- **THEN** the image destination SHALL come from explicit configuration
- **AND** credentials SHALL remain supplied through environment variables
