## MODIFIED Requirements

### Requirement: Centralized environment access and forwarding

The configuration boundary SHALL provide typed fields for known application settings and a controlled Kestrix-prefixed environment map for child processes or dispatch. Application code SHALL read known settings from those typed fields rather than passing environment variable names as string keys. Code that forwards variables SHALL use the canonical map rather than scanning legacy names directly.

Task processes started by a runner are not child processes in this sense: they SHALL NOT receive the Kestrix process environment or the canonical Kestrix-prefixed map. They SHALL receive only the allowlist defined by the `task-execution-isolation` capability, built through one function at the configuration boundary so that no executor assembles a task environment on its own.

#### Scenario: Dispatch context uses canonical names

- **WHEN** Kestrix forwards triggered-task or changed-repository variables to a child pipeline
- **THEN** the forwarded Kestrix-owned variables SHALL use the `KESTRIX_*` prefix
- **AND** legacy names SHALL not produce forwarded values

#### Scenario: No direct process reads remain

- **WHEN** the source tree is checked for runtime environment access
- **THEN** direct `os.Getenv`, `os.LookupEnv`, and raw environment-prefix scans SHALL be confined to the configuration boundary and test fixtures

#### Scenario: Known setting uses a typed field

- **WHEN** application code needs the webhook secret, GitLab token, database URL, or Docker image
- **THEN** it SHALL read the corresponding typed field from the environment/configuration object
- **AND** the environment variable name SHALL appear only in the loader's alias mapping

#### Scenario: Task process does not inherit the process environment

- **WHEN** any runner executor starts a task process or container
- **THEN** the environment it passes SHALL come from the task-environment function of the configuration boundary
- **AND** SHALL NOT be the full process environment or the Kestrix-prefixed map
