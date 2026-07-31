# runtime-configuration Specification

## Purpose
TBD - created by archiving change centralize-runtime-environment. Update Purpose after archive.
## Requirements
### Requirement: Canonical Kestrix environment names

Kestrix-owned runtime environment variables SHALL use the `KESTRIX_` prefix. The `.env.template`, CLI help, Docker examples, scripts, and documentation SHALL advertise canonical `KESTRIX_*` names. Generic variables such as `DATABASE_URL` and `DOCKER_*` and provider-owned variables SHALL retain their established names unless explicitly classified as Kestrix-owned.

#### Scenario: Environment template uses the product identity

- **WHEN** a developer copies `.env.template`
- **THEN** Kestrix-owned settings SHALL be represented with `KESTRIX_*` names
- **AND** the template SHALL not instruct new users to define `METACI_*` names

#### Scenario: Legacy environment is ignored

- **WHEN** only a legacy `METACI_*` variable exists for a migrated setting
- **THEN** Kestrix SHALL leave the corresponding canonical setting unset
- **AND** the legacy value SHALL not be normalized, forwarded, or used

### Requirement: Startup-initialized immutable environment singleton

The process SHALL initialize a single immutable environment configuration snapshot before command or server work begins. Application packages SHALL obtain environment-backed values through that snapshot or its configuration API and SHALL NOT read the process environment directly.

#### Scenario: Values are loaded once at startup

- **WHEN** the Kestrix process starts
- **THEN** the environment snapshot SHALL be initialized before subcommand execution
- **AND** subsequent configuration consumers SHALL read the same snapshot

#### Scenario: Environment changes after initialization

- **WHEN** the operating-system environment is changed after initialization
- **THEN** existing Kestrix consumers SHALL continue using the initialized snapshot
- **AND** no dynamic reload SHALL occur implicitly

### Requirement: Deterministic configuration precedence

For each migrated setting, explicit CLI flags SHALL override config-file values, config-file values SHALL override canonical `KESTRIX_*` environment values, and application defaults SHALL be used only when no higher-precedence value exists. Legacy `METACI_*` names SHALL not participate in precedence.

#### Scenario: Legacy names do not override canonical settings

- **WHEN** both `KESTRIX_WEBHOOK_SECRET` and `METACI_WEBHOOK_SECRET` are set
- **THEN** the value from `KESTRIX_WEBHOOK_SECRET` SHALL be used
- **AND** the legacy secret value SHALL not be used or forwarded

#### Scenario: CLI overrides environment

- **WHEN** a CLI flag and an environment variable provide different values
- **THEN** the explicit CLI value SHALL be used

### Requirement: Centralized environment access and forwarding

The configuration boundary SHALL provide typed fields for known application settings and a controlled Kestrix-prefixed environment map for child processes or dispatch. Application code SHALL read known settings from those typed fields rather than passing environment variable names as string keys. Code that forwards variables SHALL use the canonical map rather than scanning legacy names directly.

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

### Requirement: Secrets remain private

The singleton, diagnostics, and API layers SHALL not log, serialize, or expose secret values loaded from the environment. Compatibility warnings MAY name a deprecated variable but SHALL omit its value.

#### Scenario: Legacy secret is ignored

- **WHEN** a legacy secret variable is used
- **THEN** it SHALL have no effect on application configuration or child-process environment

