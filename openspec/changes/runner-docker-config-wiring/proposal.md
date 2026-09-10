## Why

`runner-execution-isolation` added five Docker-hardening settings
(`RunnerDockerPIDs`, `RunnerDockerMemory`, `RunnerDockerCPUs`,
`RunnerDockerUser`, `RunnerDockerNetwork`) to both `Environment`
(env-var precedence) and the `runner:` block of `internal/config/Config`
(config-file precedence), following the same dual-source pattern as
`source_cache`. `ApplyRunnerEnvironment` (`internal/config/config.go:302-331`)
is the function that reconciles the two, but nothing in `cmd/kestrix/main.go`
calls it — `DockerExecutor` is built straight from `config.Current()`
(`internal/runner/runner.go:450-461`), bypassing `Config` entirely. Today
`ApplyRunnerEnvironment` is only invoked from `config_test.go`. An operator
who sets `runner.docker_pids` (or any of the other four) in `config.yaml`
sees no effect — only the matching `KESTRIX_RUNNER_DOCKER_*` environment
variable works, silently contradicting the config file the operator just
edited.

## What Changes

- `cmd/kestrix/main.go` calls `ApplyRunnerEnvironment` after loading
  `config.yaml` and before constructing the runner, so a value set in
  `runner:` takes effect exactly like its `KESTRIX_RUNNER_DOCKER_*`
  equivalent when the environment variable is unset.

## Non-goals

- Changing the precedence rule itself (env var wins when both are set,
  matching `source_cache`) — only making the config-file side of it actually
  run.
- Adding new Docker-hardening settings; the five that already exist are the
  full scope.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `runtime-configuration`: the requirement describing config-file/env-var
  precedence for runner settings currently documents behavior
  `ApplyRunnerEnvironment` implements but the server never calls; the
  requirement must reflect that the config file path is actually wired in.

## Impact

- `cmd/kestrix/main.go`: one added call to `ApplyRunnerEnvironment` in the
  startup sequence.
- No change to `internal/config/config.go` or `internal/runner/runner.go`;
  both already do the right thing once the seam is invoked.
