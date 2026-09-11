## Why

`runner-execution-isolation` added five Docker-hardening settings
(`RunnerDockerPIDs`, `RunnerDockerMemory`, `RunnerDockerCPUs`,
`RunnerDockerUser`, `RunnerDockerNetwork`) plus `RunnerCancelGrace` to both
`Environment` (env-var precedence) and the `runner:` block of
`internal/config/Config` (config-file precedence), following the same
dual-source pattern as `source_cache`. `ApplyRunnerEnvironment`
(`internal/config/config.go:305-331`) is the function that reconciles the two,
but nothing in `cmd/kestrix/main.go` calls it — and even calling it would not be
enough on its own, because it writes into `Config.Runner`, which no consumer
reads. The runner builds `DockerExecutor` straight from the `Environment`
snapshot returned by `config.Current()` (`internal/runner/runner.go:451-460`),
and reads `RunnerCancelGrace` from that same snapshot at three further sites
(`internal/runner/runner.go:260, 294, 619`). Today `ApplyRunnerEnvironment` is
only invoked from `config_test.go`.

An operator who sets `runner.docker_pids` (or any of the other five) in
`config.yaml` sees no effect — only the matching `KESTRIX_RUNNER_*` environment
variable works, silently contradicting the config file the operator just edited.

## What Changes

- `internal/config/environment.go` gains a way to produce an `Environment`
  snapshot carrying the reconciled runner settings, and to install it as the
  process snapshot during startup — see design.md D1 (Option B).
- `cmd/kestrix/main.go` calls `ApplyRunnerEnvironment` after loading
  `config.yaml` and then installs the reconciled snapshot, before the server and
  runner are constructed, so a value set in `runner:` takes effect exactly like
  its `KESTRIX_RUNNER_*` equivalent when the environment variable is unset.
- The six settings covered are `runner.docker_pids`, `runner.docker_memory`,
  `runner.docker_cpus`, `runner.docker_user`, `runner.docker_network` and
  `runner.cancel_grace` — see design.md D2.

## Non-goals

- Changing the precedence rule itself (the config file wins when both are set,
  matching `source_cache` and `runtime-configuration`) — only making the
  config-file side of it actually run.
- Adding new Docker-hardening settings; the six that already exist are the full
  scope.
- Changing `internal/runner` or `internal/domain`; the chosen approach leaves
  both untouched (design.md D1, Options C and D rejected).
- Wiring `config.yaml` into the `kestrix run` CLI subcommand, which never loads
  configuration at all — see design.md D3.
- Reconciling the same "modelled, never consumed" defect class for the other
  settings that have it. Verified at the MR head (non-test readers), these are
  explicitly out of scope and left for follow-up work:
  - `KESTRIX_ADDR` → `Environment.ListenAddr`: no reader; runtime uses only
    `cfg.ListenAddr`/`-addr`.
  - `KESTRIX_DIR` → `Environment.PipelineDir`: no reader anywhere.
  - `KESTRIX_GITLAB_URL` → `Environment.GitLabURL`: no reader; `cmdServe`
    only takes `-gitlab-url` / `dispatch.gitlab_url`.
  - yaml `database_url` → `Config.DatabaseURL`: never copied into the runtime
    config (`web.Config.DatabaseURL` comes from the flag/`DATABASE_URL`).
  - yaml `runner.executor` → `web.RunnerConfig.Executor`: set at
    `cmd/kestrix/main.go:318`, read nowhere in `internal/web` or
    `internal/runner`.
  - `ApplyRunnerEnvironment` itself is reachable only from `config_test.go`
    today; this change adds its first production call site but does not audit
    the remaining env-only/plumbing gaps above.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `runtime-configuration`: the requirement describing config-file/env-var
  precedence for runner settings currently documents behavior
  `ApplyRunnerEnvironment` implements but the server never reaches; the
  requirement must reflect that the config file path is actually wired in, for
  `runner.cancel_grace` as well as the five Docker settings.

## Impact

- `internal/config/environment.go`: a derived-snapshot helper and a
  startup-only replacement of the process snapshot.
- `cmd/kestrix/main.go`: the reconciliation call plus installing the reconciled
  snapshot in the startup sequence.
- No change to `internal/config/config.go`, `internal/runner/runner.go` or
  `internal/domain/ports.go`.
