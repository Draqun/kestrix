# runtime-configuration Specification

## Purpose
Defines unified runtime configuration mechanisms, environment variable resolution, and canonical `KESTRIX_*` parameter handling.
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

#### Scenario: Packages read snapshot API
- **WHEN** an application package needs environment settings
- **THEN** it SHALL read from the snapshot rather than the host environment

### Requirement: Single-threaded startup snapshot replacement

Exactly one startup-time replacement of the snapshot is permitted: after configuration is loaded and before the server and runner are constructed — while still single-threaded — the process MAY install one derived snapshot carrying the reconciled runner settings. Initialization MUST have run before the replacement (otherwise the replacement is lost), and after the replacement all configuration consumers SHALL read the replaced snapshot.
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

For each migrated setting, explicit CLI flags SHALL override config-file values, config-file values SHALL override canonical `KESTRIX_*` environment values, and application defaults SHALL be used only when no higher-precedence value exists. Legacy `METACI_*` names SHALL not participate in precedence.

#### Scenario: CLI flag overrides environment setting
- **WHEN** a CLI flag and environment variable are both provided
- **THEN** the CLI flag value SHALL be used

### Requirement: Uniform startup reconciliation path

Precedence SHALL be applied at startup to every setting that has more than one form, through one reconciliation path: the config file is read first and the keys it sets are recorded; an environment value SHALL fill only a key the file did not set; a non-empty CLI flag SHALL override both. A setting SHALL NOT take a different rule because of where in the startup sequence it is read.

#### Scenario: Config file fills unset CLI flags
- **WHEN** a setting is present in config file and unset on CLI
- **THEN** config file value SHALL take precedence over environment

### Requirement: Precedence coverage across settings

This precedence rule SHALL cover Docker runner hardening settings (`runner.docker_pids`, `runner.docker_memory`, `runner.docker_cpus`, `runner.docker_user`, `runner.docker_network`, `KESTRIX_RUNNER_DOCKER_*`), runner cancellation grace period (`runner.cancel_grace`, `KESTRIX_RUNNER_CANCEL_GRACE`), `webhook.secret`, `webhook.history_limit`, `dispatch.project_id`, `dispatch.gitlab_token`, `dispatch.gitlab_url`, `listen_addr`, `pipeline.dir`, `database_url`, and `source_cache.*`.

#### Scenario: Docker hardening settings follow precedence
- **WHEN** Docker runner hardening settings are configured across file and environment
- **THEN** precedence rules SHALL determine the active values
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

The settings `runner.max_concurrent_runs` (`KESTRIX_RUNNER_MAX_CONCURRENT_RUNS`), `runner.run_history_limit` (`KESTRIX_RUN_HISTORY_LIMIT`), and `runner.max_output_bytes` (`KESTRIX_RUNNER_MAX_OUTPUT_BYTES`) SHALL be resolved by the same precedence as every other multi-form setting (explicit flag, then config file, then environment, then default) and SHALL appear in the startup source report.

#### Scenario: Run history limit precedence
- **WHEN** `KESTRIX_RUN_HISTORY_LIMIT` is set and config file omits it
- **THEN** the environment value SHALL be used

### Requirement: Run scheduling settings positive integer validation

The settings `runner.max_concurrent_runs`, `runner.run_history_limit`, and `runner.max_output_bytes` SHALL each fail startup with an error naming the setting when the resolved value is not a positive integer.
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

The settings `metrics.enabled` (`KESTRIX_METRICS_ENABLED`), `metrics.token` (`KESTRIX_METRICS_TOKEN`), and `logging.level` (`KESTRIX_LOG_LEVEL`) SHALL be resolved by the same precedence as every other multi-form setting (explicit flag, then config file, then environment, then default), SHALL appear in the startup source report, and SHALL fail startup with an error naming the setting when the resolved value is not one of its accepted values.

#### Scenario: Observability settings precedence
- **WHEN** `metrics.enabled` is set in config file
- **THEN** it SHALL take precedence over environment

### Requirement: Metrics token privacy in reports

`metrics.token` is a secret: its value SHALL NOT appear in the source report, in any log line, or in any API response.
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

