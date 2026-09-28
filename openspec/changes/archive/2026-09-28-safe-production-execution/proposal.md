## Why

In the shipped production deployment every pipeline task can read
Kestrix's master key and JWT signing key.

- The default `runner.mode` is `local` (`internal/config/config.go`), and
  `docker/docker-compose.prod.yml` does not change it.
- That compose file mounts both key files into the `kestrix` container. It
  mounts no Docker socket, and the runtime image has no `docker` CLI, so
  `docker` mode cannot run there at all.
- The image has no `USER`, so Kestrix runs as root. `LocalExecutor` runs each
  task as `bash -c` in the same container, as the same user (only `Setpgid`).

A pipeline author can therefore `cat` both key files: anyone who can push
the pipeline file, or an operator in the pipeline editor. With the master
key they decrypt every stored credential and secret. With the JWT key they
forge an administrator session. The environment allowlist blocks the key
*paths*, not the files.

`SECURITY.md` lists `local` mode as a known limitation and points to
`docker` for untrusted pipelines. The production deployment offers no
`docker`, and the impact is taking over the instance, not just "host
access". `webhook-starts-run` will make every push run a pipeline, so this
must land first.

## What Changes

- **`local` execution inside the control plane is opt-in.** A new setting,
  `runner.allow_local` (`KESTRIX_RUNNER_ALLOW_LOCAL`, default `false`),
  guards it:
  - startup fails when the instance mode is `local` without it, with a
    message naming the key exposure and the two ways out;
  - the project API rejects `mode: local`;
  - a stored `local` project's run fails before any task executes.
- **Production executes tasks on a runner, not in the control plane.**
  `docker-compose.prod.yml` sets the instance mode to `remote`. A new
  `runner` service (profile `runner`, same image, `kestrix runner`) runs with
  no key files, no database credentials, and only a network it shares with
  `kestrix`, not with `postgres`. Its token comes from Settings → Runners, a
  step documented in `docs/deployment.md`.
- The development compose file, `make serve`/`make dev`, and the test
  harnesses set `KESTRIX_RUNNER_ALLOW_LOCAL=true` explicitly.
- `SECURITY.md` and `docs/ci-tools.md` state the key exposure plainly.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `task-execution-isolation`: `local` execution requires an explicit opt-in.
- `production-deployment`: tasks run on a keyless runner; the image's shell
  requirement is restated for that.

## Non-goals

- Running `local` tasks under a separate uid inside the control plane
  (defence in depth for a later change).
- `docker` mode inside the production container (would need the Docker
  socket, which is root on the host).
- Automatic runner registration (a runner token still comes from an
  administrator).
- Moving existing `local` projects automatically. Kestrix is unpublished;
  they fail with a clear error until edited.

## Impact

- **BREAKING** for anyone running `kestrix serve` in `local` mode: set
  `KESTRIX_RUNNER_ALLOW_LOCAL=true` on a single-user machine, or use
  `remote`/`docker`.
- Compose: `runner` service and network in `docker-compose.prod.yml`;
  `KESTRIX_RUNNER_ALLOW_LOCAL=true` in `docker-compose.yml`.
- Code: `internal/config`, `internal/service` (project mode check, run
  guard), `cmd/kestrix/main.go`, `Makefile`, test harnesses; docs:
  `docs/deployment.md`, `SECURITY.md`, `docs/ci-tools.md`, `CHANGELOG.md`.
