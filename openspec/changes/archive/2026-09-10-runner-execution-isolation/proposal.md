## Why

Every registered project is remote-only, so a pipeline's HCL is authored by
whoever can push to that repository, not by the Kestrix operator. Yet the
default `local` runner hands each task script the complete Kestrix process
environment (`internal/runner/runner.go:178`, `:214`, `:502` via
`config.Current().Environ()`): `DATABASE_URL`, `KESTRIX_GITLAB_TOKEN`,
`KESTRIX_WEBHOOK_SECRET`, `DOCKER_PASSWORD`, and the paths of the master key and
JWT signing key, both readable by the same service account. One malicious or
careless commit to any registered repository is full compromise of the control
plane. The Docker executor (`runner.go:130`) adds no capability, privilege,
process, memory, or CPU limit, and cancelling a run (`PGStore.CancelRun`) only
rewrites status while the task processes keep running.

## What Changes

- **BREAKING**: task processes in every runner mode receive an explicit
  allowlist instead of the host environment: a fixed base set, the `CI_*`
  variables Kestrix sets itself, resolved pipeline variables, and only those
  host variables named in the new `KESTRIX_RUNNER_ENV_PASSTHROUGH`. Key paths,
  `DATABASE_URL`, the GitLab token, and the webhook secret can never be passed
  through. Pipelines that relied on inherited host variables (including this
  repository's own `scripts/docker-push.sh`) must list them.
- Docker tasks run with `--cap-drop ALL`, `--security-opt no-new-privileges`, a
  PID limit, configurable memory and CPU limits, an optional non-root user, and
  a configurable network, defaulting to `bridge` so deploy tasks keep egress.
- Cancelling a run terminates its processes: the local executor kills the
  task's process group with a grace period, the Docker executor stops the
  container by its deterministic name, and the run record reflects the
  termination only after it happened.
- The threat model is restated: pipeline definitions are untrusted input,
  trusted only as far as the weakest committer of the repository they live in.

## Non-goals

- Changing the default runner mode from `local` to `docker`. That is a
  deployment decision with its own migration; this change makes `local`
  survivable first.
- Runtime secret injection for tasks. A separate `runtime-secrets` change
  replaces the passthrough list as the way deploy credentials reach a task.
- Sandboxing the `local` executor beyond the environment (seccomp, chroot,
  user namespaces). The Docker mode is the isolation path.
- Resource limits for the `local` executor and for `gitlab-runner` mode.
- Mounting or proxying the Docker socket into task containers.

## Capabilities

### New Capabilities

- `task-execution-isolation`: what a task process may see and do, what
  isolation the Docker executor guarantees, and what cancellation terminates.

### Modified Capabilities

- `runtime-configuration`: the requirement "Centralized environment access and
  forwarding" currently permits forwarding the canonical Kestrix environment to
  child processes; task processes become an explicit exception with an
  allowlist.

## Impact

- `internal/runner/runner.go`: `LocalExecutor`, `DockerExecutor`, and
  `GitLabRunnerExecutor` build their environment from one shared function;
  Docker arguments gain the hardening flags; commands get a process group and a
  cancel function.
- `internal/service/service.go` (`RunService`): per-run cancellable context and
  a `Cancel(runID)` path that `handleCancelRun` calls before touching the store.
- `internal/config/environment.go`, `internal/config/config.go`,
  `.env.template`, both Compose files: `KESTRIX_RUNNER_ENV_PASSTHROUGH`,
  `KESTRIX_RUNNER_DOCKER_MEMORY`, `KESTRIX_RUNNER_DOCKER_CPUS`,
  `KESTRIX_RUNNER_DOCKER_PIDS`, `KESTRIX_RUNNER_DOCKER_USER`,
  `KESTRIX_RUNNER_DOCKER_NETWORK`, `KESTRIX_RUNNER_CANCEL_GRACE`.
- `ci/pipeline.tf` dogfood run: `scripts/docker-build.sh`, `docker-push.sh`,
  `deploy.sh`, and `test.sh` read `DOCKER_*`, `DEPLOY_HOST`, and
  `KESTRIX_TEST_DATABASE_URL` from the host. The deployment must set
  `KESTRIX_RUNNER_ENV_PASSTHROUGH` for them until `runtime-secrets` lands.
- `internal/runner` stays at 100% unit coverage (`runner-unit-only-coverage`);
  process-group kills need an injectable seam to be unit-testable.
- `openspec/design.md` "pipeline definitions are trusted" is superseded.
