# task-execution-isolation Specification

## Purpose
Defines the boundary between the Kestrix control plane and the task scripts it
executes: which environment a task may observe, what isolation a containerized
task is guaranteed, and what a cancellation terminates. Pipeline definitions
are untrusted input, trusted only as far as the least trusted committer of the
repository they are read from.

## Requirements

### Requirement: Task environment is an explicit allowlist

A task process in every runner mode SHALL receive only: a fixed base set of
host variables needed to execute a shell (`PATH`, `HOME`, `LANG`, `LC_ALL`,
`TMPDIR`, `TERM`, `USER`), the `CI_*` variables Kestrix sets for the task,
the pipeline variables resolved for that task, and host variables whose exact
names appear in `KESTRIX_RUNNER_ENV_PASSTHROUGH`. Every other variable of the
Kestrix process SHALL be withheld. The variables `KESTRIX_MASTER_KEY_PATH`,
`KESTRIX_JWT_SIGNING_KEY_PATH`, `DATABASE_URL`,
`KESTRIX_GITLAB_TOKEN`, and `KESTRIX_WEBHOOK_SECRET` SHALL never reach a task,
even when listed for passthrough; listing one SHALL be rejected at startup as a
configuration error. A pipeline variable SHALL override a base or passthrough
variable of the same name.

#### Scenario: Control-plane secrets are withheld

- **WHEN** a task script prints its environment in `local`, `docker`, or `gitlab-runner` mode
- **THEN** `DATABASE_URL`, every `KESTRIX_*` variable, and `DOCKER_PASSWORD` SHALL be absent unless explicitly passed through
- **AND** `PATH`, `HOME`, and `CI_PROJECT_DIR` SHALL be present

#### Scenario: Operator passes a host variable through

- **WHEN** `KESTRIX_RUNNER_ENV_PASSTHROUGH` is `DOCKER_REGISTRY,DEPLOY_HOST` and both are set on the host
- **THEN** a task SHALL observe both with their host values
- **AND** SHALL NOT observe `DOCKER_PASSWORD`

#### Scenario: Denied variable in the passthrough list

- **WHEN** `KESTRIX_RUNNER_ENV_PASSTHROUGH` names `DATABASE_URL` or `KESTRIX_MASTER_KEY_PATH`
- **THEN** Kestrix SHALL refuse to start and SHALL name the offending variable without printing its value

#### Scenario: Pipeline variable wins

- **WHEN** a pipeline declares a variable named `TERM` for a task
- **THEN** the task SHALL observe the pipeline value, not the host value

### Requirement: Docker tasks run with reduced privilege and bounded resources

A task executed in `docker` mode SHALL run with all Linux capabilities dropped,
with privilege escalation disabled, with a process-count limit, and with a
memory limit and CPU limit taken from configuration (`KESTRIX_RUNNER_DOCKER_PIDS`,
`KESTRIX_RUNNER_DOCKER_MEMORY`, `KESTRIX_RUNNER_DOCKER_CPUS`, each with a
non-zero default). It SHALL attach to the network named by
`KESTRIX_RUNNER_DOCKER_NETWORK` (default `bridge`) and, when
`KESTRIX_RUNNER_DOCKER_USER` is set, SHALL run as that user. Kestrix SHALL NOT
mount the Docker socket, SHALL NOT pass `--privileged`, and SHALL mount only the
run workspace and the acquired source directories.

#### Scenario: Default container hardening

- **WHEN** a task container is started with default configuration
- **THEN** the container SHALL have no capabilities, no new privileges, a PID limit, a memory limit, and a CPU limit
- **AND** `/var/run/docker.sock` SHALL NOT be mounted

#### Scenario: Network isolation opt-in

- **WHEN** `KESTRIX_RUNNER_DOCKER_NETWORK` is `none`
- **THEN** a task container SHALL have no network access
- **AND** a task that needs egress SHALL fail inside the container rather than reach the host network

#### Scenario: Limit exceeded

- **WHEN** a task allocates more memory than the configured limit
- **THEN** the container SHALL be terminated by the runtime
- **AND** the task SHALL be recorded as `failed` with the termination in its output

### Requirement: Cancellation terminates task processes

Cancelling a run SHALL stop the run's executing task before its status is
recorded as `canceled`, regardless of how the run's task was started: a
full-DAG run, a single-task re-run, or an approved manual task all use the
same termination path. In `local` mode the task's whole process group SHALL
receive a termination signal, then a kill signal after
`KESTRIX_RUNNER_CANCEL_GRACE` (default `10s`). In `docker` mode the task
container SHALL be stopped with the same grace period. Tasks not yet started
SHALL NOT start. A run whose process could not be terminated SHALL stay
`running` with the failure in its error, rather than being reported as
`canceled`.

#### Scenario: Cancel a running local task

- **WHEN** a run is cancelled while a `local` task whose script spawned child processes is executing
- **THEN** the task process and every process in its group SHALL exit within the grace period plus the kill
- **AND** the run SHALL be `canceled` and no later task in the DAG SHALL start

#### Scenario: Cancel a running Docker task

- **WHEN** a run is cancelled while a `docker` task is executing
- **THEN** the task's container SHALL no longer be running after the grace period
- **AND** the run SHALL be `canceled`

#### Scenario: Cancel with no running process

- **WHEN** a run is cancelled while it waits for manual approval and no task process exists
- **THEN** the run SHALL be `canceled` as before with no termination attempted

#### Scenario: Termination fails

- **WHEN** the runtime rejects the stop request for a task container
- **THEN** the run SHALL remain `running`
- **AND** the cancel request SHALL fail with the runtime's error, without exposing host paths or secrets

#### Scenario: Cancel a running single-task re-run

- **WHEN** a run is cancelled while a task started by the single-task re-run route is executing
- **THEN** the task's process group SHALL receive a termination signal, then a kill signal after the grace period, by the same rule as a `local`-mode DAG task
- **AND** the run SHALL be `canceled`

#### Scenario: Cancel an executing approved manual task

- **WHEN** a manual task has been approved and its process is executing, and the run is cancelled before that process exits
- **THEN** the task's process group SHALL receive a termination signal, then a kill signal after the grace period, by the same rule as a `local`-mode DAG task
- **AND** the run SHALL be `canceled` rather than left `running` with the process still active
