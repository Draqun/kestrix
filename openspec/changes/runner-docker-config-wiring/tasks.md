## 1. Wire the config-file path into startup

- [ ] 1.1 In `cmd/kestrix/main.go`, call `config.ApplyRunnerEnvironment(...)` (or the appropriate exported entry point) after `config.Load` succeeds and before `DockerExecutor`/the runner is constructed; verify a startup-level test asserts a `runner.docker_memory` value set only in `config.yaml` is reflected in the constructed executor's settings.
- [ ] 1.2 Verify the existing `config_test.go` coverage for `ApplyRunnerEnvironment` still passes unchanged (its precedence logic is not being modified, only its call site is being added).

## 2. Regression coverage

- [ ] 2.1 Add a test with both a config-file value and a differing `KESTRIX_RUNNER_DOCKER_*` env var set, asserting the config-file value wins end to end (through to the constructed `DockerExecutor`), matching `runtime-configuration`'s precedence rule.
- [ ] 2.2 Run `go build ./... && go vet ./...` and the full `internal/config` and `internal/runner` unit suites.
