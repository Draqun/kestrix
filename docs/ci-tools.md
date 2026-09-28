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

Executes pipeline tasks in one of three modes, set per project (empty
defers to the instance's own `runner.mode`, `local` unless configured
otherwise):

| Mode | Description |
|------|-------------|
| `local` | Runs tasks as bash subprocesses in this same process - can read its master key and JWT signing key, so it executes only when `runner.allow_local` (`KESTRIX_RUNNER_ALLOW_LOCAL`) is true; see [Known limitations](../SECURITY.md#known-limitations-not-vulnerabilities) |
| `docker` | Executes each task in a Docker container |
| `remote` | Dispatches each task as a job for a registered runner to claim and execute on a separate machine or container - see [`remote-runners.md`](remote-runners.md) |

The instance's own `runner.mode` has an env form, `KESTRIX_EXECUTION_MODE`
- distinct from `KESTRIX_RUNNER_MODE`, which configures only `kestrix
runner`'s own execution backend (`local` or `docker`) on the machine
running that subcommand, not the server.

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

| Setting | Env var | Config file | Default |
|---------|---------|-------------|---------|
| Process limit | `KESTRIX_RUNNER_DOCKER_PIDS` | `runner.docker_pids` | `256` |
| Memory limit | `KESTRIX_RUNNER_DOCKER_MEMORY` | `runner.docker_memory` | `2g` |
| CPU limit | `KESTRIX_RUNNER_DOCKER_CPUS` | `runner.docker_cpus` | `2` |
| Network | `KESTRIX_RUNNER_DOCKER_NETWORK` | `runner.docker_network` | `bridge` (`none` disables egress) |
| User | `KESTRIX_RUNNER_DOCKER_USER` | `runner.docker_user` | image default (unset) |

`--init`, `--cap-drop ALL` and `--security-opt no-new-privileges` are always applied.
`--init` runs a minimal init as the container's PID 1, so cancelling a task
delivers `SIGTERM` to the script instead of waiting out the full stop grace.
Kestrix never mounts the Docker socket or passes `--privileged` into a task
container.

### Remote mode

On a `remote`-mode project, a single-task re-run and an approved manual gate
dispatch a job to a runner rather than executing on the Kestrix host -
asynchronous, no local process. An approval does not mean the task has
started: it means a job now exists for a runner to claim, which may not
happen until a runner next polls.

A `remote`-mode run's trace may interleave two tasks' lines unpredictably.
Every other mode has one sequential writer owning a run's output, so its
lines stay in execution order; `remote` mode can have two jobs of one run
streaming from two different runners at the same time, and only that no
output is lost is guaranteed - not the order two concurrent tasks' lines
appear in.

### Cancellation

Cancelling a run terminates its executing task before the run is recorded as
`canceled`: the `local` executor sends the task's whole
process group SIGTERM, then SIGKILL after `KESTRIX_RUNNER_CANCEL_GRACE`
(default `10s`, also `runner.cancel_grace` in the config file); the `docker` executor stops the container with the same
grace period. A run whose task cannot be confirmed terminated within grace
plus 5s stays `running` and the cancel request fails, rather than reporting
`canceled` while the task keeps executing.

`remote` mode cancels differently: Kestrix has no inbound channel to a
runner (it only ever polls outbound - the whole point of working from
behind NAT), so there is no way to send a stop signal. Cancelling a
`remote`-mode run marks it `canceled` immediately, without waiting on any
`claimed`/`running` job the way the grace-period wait above does - but a
task already running on a runner keeps running to completion there. That
process's late output still arrives over the runner-job protocol and is
still stored, but it does not reopen the run, change its `canceled` status,
or supersede the `canceled` task result already recorded. This is a
deliberate carve-out on the `task-execution-isolation` capability's
"Cancellation terminates task processes" requirement (see that spec's
MODIFIED delta) and is specified in full under `remote-runner-execution`'s
"Cancelling a remote-mode run".

`local` mode still runs task scripts as the Kestrix service account; the
allowlist removes the obvious secrets from its environment, but it does not
make `local` mode safe for mutually untrusted pipelines on its own. `docker`
mode's isolation is the boundary to rely on for that.

## Validation

```bash
kestrix validate pipeline.tf   # validate HCL syntax and structure
kestrix resolve pipeline.tf    # resolve triggers for a set of changed repos
```
