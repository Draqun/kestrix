## 1. Task environment allowlist

- [ ] 1.1 Add `RunnerEnvPassthrough []string` to `internal/config/environment.go`, parsed from `KESTRIX_RUNNER_ENV_PASSTHROUGH` (comma-separated, trimmed, empty entries dropped), and reject `KESTRIX_MASTER_KEY_PATH`, `KESTRIX_JWT_SIGNING_KEY_PATH`, `DATABASE_URL`, `KESTRIX_GITLAB_TOKEN`, `KESTRIX_WEBHOOK_SECRET` through `ValidationError` naming the variable without its value; verify a config test covers an empty list, a valid list, and each denied name.
- [ ] 1.2 Add `(*Environment).TaskEnv(ci map[string]string, vars map[string]string) []string` emitting `PATH`, `HOME`, `LANG`, `LC_ALL`, `TMPDIR`, `TERM`, `USER` from the snapshot when present, then `ci`, then passthrough names, then `vars` last; verify a unit test asserts `KESTRIX_*`, `DATABASE_URL`, and `DOCKER_PASSWORD` are absent, a passthrough name is present, and a pipeline variable named `TERM` overrides the host value.
- [ ] 1.3 Replace `config.Current().Environ()` in `LocalExecutor.RunTask` (`internal/runner/runner.go:178`), `GitLabRunnerExecutor.RunTask` (`:214`), the `docker run -e` loop (`:143`), and the third call at `:502` with `TaskEnv`, passing `CI_PROJECT_DIR` through the `ci` map; verify `grep -n "Environ()" internal/runner` returns nothing and the existing runner unit tests still pass at 100%.
- [ ] 1.4 Print the effective allowlist names (never values) once per run before the first task starts; verify a unit test asserts the line is present and contains no `=`-separated values.

## 2. Docker executor hardening

- [ ] 2.1 Add `RunnerDockerPIDs` (default `256`), `RunnerDockerMemory` (default `2g`), `RunnerDockerCPUs` (default `2`), `RunnerDockerUser` (default empty), `RunnerDockerNetwork` (default `bridge`) to `environment.go` and the `runner:` block of `internal/config/config.go`, following the `source_cache` precedence pattern; verify config tests cover defaults, explicit values, and rejection of a non-positive PID limit.
- [ ] 2.2 Thread the five settings into `DockerExecutor` and emit `--cap-drop ALL`, `--security-opt no-new-privileges`, `--pids-limit`, `--memory`, `--cpus`, `--network`, and `--user` only when set, before the `-v` flags in `runner.go:130`; verify a unit test using the `commandContext` stub asserts every flag and its value in `cmd.Args`, and asserts `docker.sock` and `--privileged` are absent.
- [ ] 2.3 Verify a unit test asserts `--network none` is emitted verbatim when configured and that `--user` is omitted when unset.

## 3. Process-group termination in the executors

- [ ] 3.1 Add `KESTRIX_RUNNER_CANCEL_GRACE` (default `10s`) to `environment.go` and `RunnerConfig`; verify a config test covers the default, an explicit value, and rejection of a negative duration.
- [ ] 3.2 Add a package-level `killGroup = func(pgid int, sig syscall.Signal) error { return syscall.Kill(-pgid, sig) }` in `internal/runner`, set `SysProcAttr{Setpgid: true}`, `cmd.Cancel` (SIGTERM to the group), and `cmd.WaitDelay = grace` in `LocalExecutor` and `GitLabRunnerExecutor`, with a deferred timer that sends SIGKILL to the group after the grace period; verify a unit test with an injected `killGroup` records `(pgid, SIGTERM)` then `(pgid, SIGKILL)` when the context is cancelled and the fake process does not exit.
- [ ] 3.3 Set `cmd.Cancel` in `DockerExecutor` to run `docker stop -t <grace-seconds> <container-name>` through `commandContext`, reusing the name already built at `runner.go:132`; verify a unit test asserts the stop command's args on cancel and that no stop is issued when the task exits normally.
- [ ] 3.4 Add an integration test (build tag or `testing.Short()` guard, excluded from unit coverage per `runner-unit-only-coverage`) that runs `bash -c 'sleep 30 & sleep 30'`, cancels the context, and verifies both `sleep` processes are gone within grace plus two seconds.

## 4. RunService cancellation

- [ ] 4.1 Add `active map[int]context.CancelFunc` and `done map[int]chan struct{}` guarded by a mutex to `RunService` (`internal/service/service.go:158`); wrap the context in `Start` with `context.WithCancel`, register both, and remove them when `executeRun` returns; verify a service unit test asserts registration during a run and removal after.
- [ ] 4.2 Add `(*RunService).Cancel(ctx context.Context, runID int) error` that cancels, waits on the done channel up to grace plus 5 s, then calls `runStore.CancelRun`; when the wait times out it returns an error and leaves the store untouched; when no active entry exists it calls `runStore.CancelRun` directly; verify unit tests with mock stores cover all three paths.
- [ ] 4.3 Make `taskLoop` stop scheduling tasks once `ctx.Err() != nil`, recording remaining pending tasks as `canceled` instead of running them; verify a runner unit test asserts no executor call happens for a task after cancellation.
- [ ] 4.4 Change `handleCancelRun` (`internal/web/handler_run.go:298`) to call `runService.Cancel` and map a timeout error to `504` with a message that names the run but no host path; verify a handler test covers success, timeout, and unknown run.
- [ ] 4.5 Add `Cancel` to the `domain` service seam the handler uses if one exists (check `internal/domain/ports.go` and `internal/mocks`), regenerate mocks, and verify `go test ./internal/domain/...` including `portscheck` passes.

## 5. Deployment, dogfood, and documentation

- [ ] 5.1 Add all seven new variables to `.env.template`, `docker/docker-compose.yml`, and `docker/docker-compose.prod.yml`, with the dogfood passthrough list `DOCKER_REGISTRY,DOCKER_IMAGE,DOCKER_USERNAME,DOCKER_PASSWORD,DEPLOY_HOST,KESTRIX_TEST_DATABASE_URL` set in the Compose files; verify `make pipeline` completes with the new binary.
- [ ] 5.2 Update `docs/ci-tools.md` and `AGENTS.md` ("Cancel runs", "Runtime facts") to describe the allowlist, the Docker defaults, and that cancel now terminates processes; replace the "pipeline definitions are trusted" sentence in `openspec/design.md` with the untrusted-input statement; verify `make ui-build` copies the docs.
- [ ] 5.3 Add a CHANGELOG entry under Unreleased marking the environment change **BREAKING**, listing the new variables and the dogfood passthrough list.

## 6. Verification

- [ ] 6.1 Run `make quality` and verify `internal/runner` reports 100.0% unit-only coverage and `internal/web`/`internal/service` stay at their thresholds.
- [ ] 6.2 Run `make docker-e2e` and verify a task started in the deployment container cannot print `DATABASE_URL` or any `KESTRIX_*` variable, and that a cancelled run's container is absent from `docker ps`.
- [ ] 6.3 Run `npx -y @fission-ai/openspec@latest validate runner-execution-isolation --strict` and verify it passes.
