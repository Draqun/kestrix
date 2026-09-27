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

For each migrated setting, explicit CLI flags SHALL override config-file values, config-file values SHALL override canonical `KESTRIX_*` environment values, and application defaults SHALL be used only when no higher-precedence value exists. Legacy `METACI_*` names SHALL not participate in precedence. This precedence SHALL be applied at startup to every setting that has more than one form, through one reconciliation path: the config file is read first and the keys it sets are recorded; an environment value SHALL fill only a key the file did not set; a non-empty CLI flag SHALL override both. A setting SHALL NOT take a different rule because of where in the startup sequence it is read. This covers the Docker runner hardening settings (`runner.docker_pids`, `runner.docker_memory`, `runner.docker_cpus`, `runner.docker_user`, `runner.docker_network` and their `KESTRIX_RUNNER_DOCKER_*` equivalents), the runner cancellation grace period (`runner.cancel_grace` and `KESTRIX_RUNNER_CANCEL_GRACE`), `webhook.secret`, `webhook.history_limit`, `dispatch.project_id`, `dispatch.gitlab_token`, `dispatch.gitlab_url`, `listen_addr`, `pipeline.dir`, `database_url`, and `source_cache.*`.

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
  field), local execution (`LocalExecutor.RunTask`), and manual approval
  (`RunOne`) alike
- **AND** manual approval SHALL still execute on the host regardless of runner
  mode (tracked in #1)

#### Scenario: Config file overrides environment for a secret

- **WHEN** `config.yaml` sets `webhook.secret` and `KESTRIX_WEBHOOK_SECRET` is set to a different value
- **THEN** the webhook receiver SHALL verify deliveries against the config-file value
- **AND** the same rule SHALL hold for `dispatch.project_id`, `dispatch.gitlab_token`, and `webhook.history_limit`

#### Scenario: Environment fills a key the file omits

- **WHEN** `config.yaml` exists but does not set `dispatch.gitlab_token`, and `KESTRIX_GITLAB_TOKEN` is set
- **THEN** the dispatch client SHALL use the environment value
- **AND** an explicitly empty value in the file SHALL count as set and SHALL NOT be filled from the environment

#### Scenario: Previously inert forms take effect

- **WHEN** `KESTRIX_ADDR`, `KESTRIX_DIR`, or `KESTRIX_GITLAB_URL` is set and the config file does not set the matching key
- **THEN** the server SHALL listen on, read pipelines from, or dispatch to that value respectively
- **AND** a `database_url` set in `config.yaml` SHALL be used when no `-database-url` flag is given

#### Scenario: Single-form settings are unchanged

- **WHEN** a setting has only an environment form (for example `KESTRIX_MASTER_KEY_PATH`) or only a config-file form (for example `runner.mode`)
- **THEN** it SHALL be read from that form and its default exactly as before

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

- **WHEN** application code needs the webhook secret, GitLab token, or database URL
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

### Requirement: Every configuration form has a consumer

Every configuration form Kestrix accepts — a config-file key, a `KESTRIX_*` or generic environment variable modelled at the configuration boundary, or a `serve` flag — SHALL reach a runtime consumer. A form that no consumer reads SHALL be removed rather than documented. The config file SHALL be parsed with unknown keys rejected, so that a key the loader does not model fails startup with an error naming the key and file.

#### Scenario: Unknown config-file key fails startup

- **WHEN** `config.yaml` contains a key the loader does not model, for example the removed `docker:` block or `runner.executor`
- **THEN** `kestrix serve` SHALL exit non-zero before opening the listener
- **AND** the error SHALL name the offending key and the config-file path
- **AND** SHALL NOT print any value from the file

#### Scenario: Every form is exercised by one table

- **WHEN** the configuration test suite runs
- **THEN** one table SHALL contain, for every multi-form setting, a case per form asserting the value reaches the runtime configuration
- **AND** the suite SHALL fail when a typed environment field or a config-file key exists that no table entry covers

#### Scenario: Removed forms are gone from every surface

- **WHEN** an operator reads `kestrix serve -h`, `.env.template`, or the README configuration example
- **THEN** `-docker-registry`, `-docker-image`, `-docker-username`, `-docker-password`, the `docker:` block, and `runner.executor` SHALL NOT appear as Kestrix settings
- **AND** `DOCKER_*` SHALL be documented only as host variables forwarded to tasks through `KESTRIX_RUNNER_ENV_PASSTHROUGH`

### Requirement: Run scheduling settings follow the precedence rule

The settings `runner.max_concurrent_runs` (`KESTRIX_RUNNER_MAX_CONCURRENT_RUNS`), `runner.run_history_limit` (`KESTRIX_RUN_HISTORY_LIMIT`), and `runner.max_output_bytes` (`KESTRIX_RUNNER_MAX_OUTPUT_BYTES`) SHALL be resolved by the same precedence as every other multi-form setting (explicit flag, then config file, then environment, then default), SHALL appear in the startup source report, and SHALL each fail startup with an error naming the setting when the resolved value is not a positive integer.

#### Scenario: File wins over environment
- **WHEN** `config.yaml` sets `runner.max_concurrent_runs: 4` and the environment sets `KESTRIX_RUNNER_MAX_CONCURRENT_RUNS=1`
- **THEN** the effective limit is `4` and the source report attributes it to the file

#### Scenario: Environment fills an absent key
- **WHEN** the file omits `runner.run_history_limit` and the environment sets `KESTRIX_RUN_HISTORY_LIMIT=500`
- **THEN** the effective limit is `500` and the source report attributes it to the environment

#### Scenario: Invalid value
- **WHEN** any of the three resolves to `0`, a negative number, or a non-integer
- **THEN** startup fails naming that setting and never reveals other values

### Requirement: Observability settings follow the precedence rule

The settings `metrics.enabled` (`KESTRIX_METRICS_ENABLED`), `metrics.token`
(`KESTRIX_METRICS_TOKEN`), and `logging.level` (`KESTRIX_LOG_LEVEL`) SHALL be
resolved by the same precedence as every other multi-form setting (explicit
flag, then config file, then environment, then default), SHALL appear in the
startup source report, and SHALL fail startup with an error naming the setting
when the resolved value is not one of its accepted values. `metrics.token` is a
secret: its value SHALL NOT appear in the source report, in any log line, or in
any API response.

#### Scenario: File wins over environment
- **WHEN** `config.yaml` sets `logging.level: debug` and the environment sets `KESTRIX_LOG_LEVEL=warn`
- **THEN** the effective level is `debug` and the source report attributes it to the file

#### Scenario: Environment fills an absent key
- **WHEN** the file omits `metrics.enabled` and the environment sets `KESTRIX_METRICS_ENABLED=true`
- **THEN** metrics are enabled and the source report attributes the setting to the environment

#### Scenario: Invalid value
- **WHEN** `logging.level` resolves to a value outside `debug`, `info`, `warn`, `error`, or `metrics.enabled` to a non-boolean
- **THEN** startup fails naming that setting and never reveals other values

#### Scenario: Scrape token stays private
- **WHEN** `metrics.token` is set by any form
- **THEN** the startup source report SHALL name the setting and its source without its value
- **AND** no log line SHALL contain the token

### Requirement: Instance runner mode is validated

The instance-wide `runner.mode` setting SHALL accept only `local`, `docker`,
or `remote` (or be unset, meaning `local`). Any other value SHALL fail
startup naming the setting and the accepted values.

#### Scenario: Unknown mode rejected at startup
- **WHEN** Kestrix starts with `runner.mode: bogus`
- **THEN** startup SHALL fail naming `runner.mode` and the accepted values

#### Scenario: Supported modes accepted
- **WHEN** Kestrix starts with `runner.mode` unset, `local`, `docker`, or `remote`
- **THEN** startup SHALL NOT fail on the runner mode

