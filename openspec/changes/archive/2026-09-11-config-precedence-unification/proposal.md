## Why

`runtime-configuration` requires one precedence for every setting: CLI flag
over config file over `KESTRIX_*` environment over default. `kestrix serve`
implements three. Verified at `main` (`a07a54ff`):

- **Rule A, as specified** — only `source_cache.*`
  (`internal/config/config.go:283-300` plus `ApplySourceCacheFlags`).
- **Env beats file** — `webhook.secret`, `dispatch.project_id`,
  `dispatch.gitlab_token`: `cmd/kestrix/main.go:200-212` fills an empty flag
  from the environment, then `:229-245` lets the flag overwrite the file
  value. `webhook.history_limit`: `config.go:263` says so outright.
- **One form dead** — `KESTRIX_ADDR`, `KESTRIX_DIR`, `KESTRIX_GITLAB_URL` are
  parsed into `Environment` and read by nothing; yaml `database_url` is never
  read; `runner.executor` is copied to `web.RunnerConfig.Executor`
  (`main.go:318`) and read nowhere; the whole `docker.*` surface (yaml, four
  `-docker-*` flags, `Environment.Docker`, `web.DockerConfig`) is dead
  end-to-end since runtime-secrets D3.

The shared cause: each setting got its own plumbing in `cmdServe`, and no test
walks a value from file, environment, or flag to its consumer.
`runner-docker-config-wiring` fixes one instance; this change fixes the class.

## What Changes

- **BREAKING**: the config file now overrides the environment for
  `webhook.secret`, `webhook.history_limit`, `dispatch.project_id`, and
  `dispatch.gitlab_token`. Deployments without a `config.yaml` (Compose) are
  unaffected.
- One reconciliation path: `config.Load` records key presence for every yaml
  key; a single `ApplyEnvironment` fills only absent keys from the
  environment; a single `ApplyFlags` applies non-zero flags last. The three
  `Apply*Environment` functions and the env-to-flag fill in `cmdServe` go away.
- Wire the four dead forms: `KESTRIX_ADDR`, `KESTRIX_DIR`,
  `KESTRIX_GITLAB_URL`, yaml `database_url`.
- **BREAKING**: remove `runner.executor` and the `docker.*` configuration
  surface (yaml block, `-docker-*` flags, typed environment fields).
  `DOCKER_*` host variables keep reaching dogfood scripts through
  `KESTRIX_RUNNER_ENV_PASSTHROUGH` (`docker/docker-compose.yml:65`); Kestrix
  itself never used them.
- **BREAKING**: an unknown key in `config.yaml` fails startup naming the key,
  so a setting nobody reads cannot be added silently again.
- One table-driven test asserts, for every setting and every form it has, that
  the value reaches `web.Config`, and fails when a new setting is not in the
  table.

## Non-goals

- Adding new forms (no yaml for `webhook.method`, no env for `runner.mode`).
- `kestrix run` and `kestrix user` never call `config.Load`; the CLI paths
  stay environment-only, as `runner-docker-config-wiring` D3 records.
- The `gitlab-runner` runner mode. The installed `gitlab-runner` 19.3.1 has no
  `exec` command (`FATAL: Command exec not found`), so the mode cannot work
  today; that is a separate change, not a configuration one.
- Changing the runner-settings wiring itself; this change lands after
  `runner-docker-config-wiring` and folds `ApplyRunnerEnvironment` into
  `ApplyEnvironment` without changing its rule.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `runtime-configuration`: "Deterministic configuration precedence" applies to
  every multi-form setting and gains scenarios for the settings that violate
  it today; "Centralized environment access and forwarding" drops the Docker
  image from its typed-field example; a new requirement forbids configuration
  forms with no consumer and unknown config-file keys.

## Impact

- `internal/config/config.go`: presence tracking for all keys, one
  `ApplyEnvironment`, one `ApplyFlags`, `KnownFields`, removal of
  `DockerConfig` and `RunnerConfig.Executor`.
- `internal/config/environment.go`: removal of `DockerEnvironment`.
- `cmd/kestrix/main.go`: `cmdServe` shrinks to load, apply, build; a pure
  `buildServeConfig` becomes the test seam.
- `internal/web/server.go`: `DockerConfig` and `RunnerConfig.Executor` removed.
- `.env.template`, README, CHANGELOG, both Compose files: `DOCKER_*` stay as
  passthrough documentation only; `-docker-*` flags disappear from help.
