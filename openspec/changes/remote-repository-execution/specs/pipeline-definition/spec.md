## MODIFIED Requirements

### Requirement: Repository Declaration (implemented)

Each `ci_repo` declaration SHALL identify a repository source by defining exactly one of `path` or `url`. A local `path` MAY point to a monorepo subdirectory. A remote `url` SHALL be a standard Git URL and MAY specify `ref` and `credential_ref`. Credential values SHALL NOT be stored in the pipeline definition or embedded in the URL.

#### Scenario: Local monorepo subdirectory

- **WHEN** a `ci_repo` declaration contains `path = "services/api"`
- **THEN** Kestrix SHALL resolve the repository from the local project checkout
- **AND** SHALL use `services/api` as the repository working directory

#### Scenario: Remote repository declaration

- **WHEN** a `ci_repo` declaration contains `url = "ssh://git@example.test/team/auth.git"`, `ref = "v2.1.0"`, and `credential_ref = "build-auth"`
- **THEN** Kestrix SHALL acquire that Git source before executing dependent tasks
- **AND** SHALL use the requested tag
- **AND** SHALL expose only the logical repository name and non-secret source metadata to the run

#### Scenario: Invalid repository declaration

- **WHEN** a `ci_repo` declaration contains neither `path` nor `url`, or contains both
- **THEN** validation SHALL fail with an actionable declaration error
- **AND** Kestrix SHALL not start the pipeline
