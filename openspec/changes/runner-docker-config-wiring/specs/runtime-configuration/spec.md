## MODIFIED Requirements

### Requirement: Deterministic configuration precedence

For each migrated setting, explicit CLI flags SHALL override config-file values, config-file values SHALL override canonical `KESTRIX_*` environment values, and application defaults SHALL be used only when no higher-precedence value exists. Legacy `METACI_*` names SHALL not participate in precedence. This precedence SHALL be applied at startup for every setting that has both a config-file and an environment-variable form, including the Docker runner hardening settings (`runner.docker_pids`, `runner.docker_memory`, `runner.docker_cpus`, `runner.docker_user`, `runner.docker_network` and their `KESTRIX_RUNNER_DOCKER_*` equivalents) — a setting present in `config.yaml` SHALL take effect even when the server never received the matching environment variable.

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
