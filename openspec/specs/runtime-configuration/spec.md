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

The process SHALL initialize a single immutable environment configuration snapshot before command or server work begins. Application packages SHALL obtain environment-backed values through that snapshot or its configuration API and SHALL NOT read the process environment directly. Exactly one startup-time replacement of the snapshot is permitted: after configuration is loaded and before the server and runner are constructed — while still single-threaded — the process MAY install one derived snapshot carrying the reconciled runner settings. Initialization MUST have run before the replacement (otherwise the replacement is lost), and after the replacement all configuration consumers SHALL read the replaced snapshot.

#### Scenario: Values are loaded once at startup

- **WHEN** the Kestrix process starts
- **THEN** the environment snapshot SHALL be initialized before subcommand execution
- **AND** subsequent configuration consumers SHALL read the same snapshot, except for the single permitted startup-time replacement carrying the reconciled runner settings

#### Scenario: Environment changes after initialization

- **WHEN** the operating-system environment is changed after initialization
- **THEN** existing Kestrix consumers SHALL continue using the initialized snapshot
- **AND** no dynamic reload SHALL occur implicitly

#### Scenario: Single startup replacement with rebind

- **WHEN** startup installs the derived runner snapshot
- **THEN** the local snapshot handle SHALL be rebound to the derived copy
  before further use, so no two live snapshots differ in the runner fields

### Requirement: Deterministic configuration precedence

For each migrated setting, explicit CLI flags SHALL override config-file values, config-file values SHALL override canonical `KESTRIX_*` environment values, and application defaults SHALL be used only when no higher-precedence value exists. Legacy `METACI_*` names SHALL not participate in precedence. For the six runner settings this change wires — the Docker runner hardening settings (`runner.docker_pids`, `runner.docker_memory`, `runner.docker_cpus`, `runner.docker_user`, `runner.docker_network` and their `KESTRIX_RUNNER_DOCKER_*` equivalents) and the runner cancellation grace period (`runner.cancel_grace` and `KESTRIX_RUNNER_CANCEL_GRACE`) — this precedence SHALL be applied at startup: a setting present in `config.yaml` SHALL take effect even when the server never received the matching environment variable. (Other settings with config-file and environment-variable forms — e.g. `webhook.history_limit`, `webhook.secret`, `dispatch.*`, `database_url` — keep their existing precedence behaviour, which this change does not reconcile.)

#### Scenario: Legacy names do not override canonical settings

- **WHEN** both `KESTRIX_WEBHOOK_SECRET` and `METACI_WEBHOOK_SECRET` are set
- **THEN** the value from `KESTRIX_WEBHOOK_SECRET` SHALL be used
- **AND** the legacy secret value SHALL not be used or forwarded

#### Scenario: CLI overrides environment

- **WHEN** a CLI flag and an environment variable provide different values
- **THEN** the explicit CLI value SHALL be used

#### Scenario: Config-file Docker runner setting takes effect

- **WHEN** `config.yaml` sets `runner.docker_memory` and no `KESTRIX_RUNNER_DOCKER_MEMORY` environment variable is set
- **THEN** the Docker executor SHALL use the config-file value
- **AND** SHALL NOT silently fall back to the application default

#### Scenario: Config-file cancellation grace period takes effect

- **WHEN** `config.yaml` sets `runner.cancel_grace` and no `KESTRIX_RUNNER_CANCEL_GRACE` environment variable is set
- **THEN** a cancelled task SHALL be stopped using the config-file grace period
- **AND** the config-file value SHALL apply to the Docker executor (`Grace`
  field), local execution (`LocalExecutor.RunTask`), gitlab-runner execution
  (`GitLabRunnerExecutor.RunTask`), and manual approval (`RunOne`) alike
- **AND** manual approval SHALL still execute on the host regardless of runner
  mode (tracked in #1)

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

### Requirement: Secrets remain private

The singleton, diagnostics, and API layers SHALL not log, serialize, or expose secret values loaded from the environment. Compatibility warnings MAY name a deprecated variable but SHALL omit its value.

#### Scenario: Legacy secret is ignored

- **WHEN** a legacy secret variable is used
- **THEN** it SHALL have no effect on application configuration or child-process environment
