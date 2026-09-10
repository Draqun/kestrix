## Context

See proposal.md — Why. Small wiring fix; included because `tasks.md`
depends on it per the schema graph.

```
config.yaml runner:  ──┐
                        ├──► ApplyRunnerEnvironment() ──► Environment (in-memory) ──► DockerExecutor
KESTRIX_RUNNER_DOCKER_* ┘        ▲
                                   never called today (only from config_test.go)
```

`ApplyRunnerEnvironment` (`internal/config/config.go:302-331`) already
implements the precedence `runtime-configuration` requires (config-file
overrides env, same `source_cache` pattern). The only defect is that
`cmd/kestrix/main.go` never calls it.

## Goals / Non-Goals

**Goals:** the existing function runs at startup, before `DockerExecutor` is
built.

**Non-Goals:** changing the precedence rule, adding settings, or touching
`internal/runner`.

## Decisions

**D1: Call `ApplyRunnerEnvironment` from `cmd/kestrix/main.go` right after
`config.Load` and before the runner/server are constructed**, matching where
`main.go` already applies other config-to-environment reconciliation steps.
No alternative considered — this is the one missing call, not a design
choice.

## Security Considerations

None. This only makes an already-reviewed, already-tested function
(`config_test.go` covers it today) execute in production; it does not
change what values are considered valid.

## Migration Plan

No data migration. An operator with `runner.docker_pids` etc. already set in
`config.yaml` (which had no effect) will see it take effect after this
deploys — flag as a behavior change in the release notes so an operator who
set both the config value and the env var to different values on purpose
isn't surprised by which one now wins (config file, per the existing
documented precedence). Rollback is a plain revert.
