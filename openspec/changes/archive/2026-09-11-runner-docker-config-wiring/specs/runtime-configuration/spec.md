## MODIFIED Requirements

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
