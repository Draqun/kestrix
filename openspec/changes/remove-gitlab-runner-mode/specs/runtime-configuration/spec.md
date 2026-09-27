## MODIFIED Requirements

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

## ADDED Requirements

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
