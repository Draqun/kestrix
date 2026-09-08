# CI Tools

Kestrix includes tooling for generating GitLab CI configuration, dispatching pipelines, and running tasks across multiple execution environments.

## CI Generator

Converts a resolved Kestrix pipeline into a standard `gitlab-ci.yml` file, enabling integration with GitLab's CI/CD runner ecosystem.

```bash
kestrix generate path/to/pipeline.tf
```

## Dispatch Client

Triggers GitLab pipelines from the command line or from within a Kestrix task execution. Uses the GitLab API to start a pipeline with the generated configuration.

## Runner

Executes pipeline tasks in one of three modes:

| Mode | Description |
|------|-------------|
| `local` (default) | Runs tasks as bash subprocesses on the host |
| `docker` | Executes each task in a Docker container |
| `gitlab-runner` | Delegates execution to a GitLab Runner |

Pipeline definitions are untrusted input, trusted only as far as the least
trusted committer of the repository they are read from - anyone who can push
a task script controls what that task's process can attempt.

### Task environment allowlist

A task process in every mode receives an explicit allowlist, never the
Kestrix process environment:

- A fixed base set needed to run a shell: `PATH`, `HOME`, `LANG`, `LC_ALL`,
  `TMPDIR`, `TERM`, `USER`.
- The `CI_*` variables Kestrix sets for the task (e.g. `CI_PROJECT_DIR`).
- The pipeline's own resolved variables, which win on a name collision.
- Host variables named in `KESTRIX_RUNNER_ENV_PASSTHROUGH` (comma-separated).

`KESTRIX_MASTER_KEY_PATH`, `KESTRIX_JWT_SIGNING_KEY_PATH`, `DATABASE_URL`,
`KESTRIX_GITLAB_TOKEN`, and `KESTRIX_WEBHOOK_SECRET` can never be listed for
passthrough; Kestrix refuses to start if they are. A pipeline that relies on
an inherited host variable must list it explicitly - see `.env.template`.

### Docker mode hardening

`docker` mode runs every task container with capabilities dropped, privilege
escalation disabled, and bounded resources by default:

| Setting | Env var | Default |
|---------|---------|---------|
| Process limit | `KESTRIX_RUNNER_DOCKER_PIDS` | `256` |
| Memory limit | `KESTRIX_RUNNER_DOCKER_MEMORY` | `2g` |
| CPU limit | `KESTRIX_RUNNER_DOCKER_CPUS` | `2` |
| Network | `KESTRIX_RUNNER_DOCKER_NETWORK` | `bridge` (`none` disables egress) |
| User | `KESTRIX_RUNNER_DOCKER_USER` | image default (unset) |

`--cap-drop ALL` and `--security-opt no-new-privileges` are always applied.
Kestrix never mounts the Docker socket or passes `--privileged` into a task
container.

### Cancellation

Cancelling a run terminates its executing task before the run is recorded as
`canceled`: the `local` and `gitlab-runner` executors send the task's whole
process group SIGTERM, then SIGKILL after `KESTRIX_RUNNER_CANCEL_GRACE`
(default `10s`); the `docker` executor stops the container with the same
grace period. A run whose task cannot be confirmed terminated within grace
plus 5s stays `running` and the cancel request fails, rather than reporting
`canceled` while the task keeps executing.

`local` mode still runs task scripts as the Kestrix service account; the
allowlist removes the obvious secrets from its environment, but it does not
make `local` mode safe for mutually untrusted pipelines on its own. `docker`
mode's isolation is the boundary to rely on for that.

## Validation

```bash
kestrix validate pipeline.tf   # validate HCL syntax and structure
kestrix resolve pipeline.tf    # resolve triggers for a set of changed repos
```
