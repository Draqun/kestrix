# task-execution-isolation Specification

## Purpose
Defines the boundary between the Kestrix control plane and the task scripts it
executes: which environment a task may observe, what isolation a containerized
task is guaranteed, and what a cancellation terminates. Pipeline definitions
are untrusted input, trusted only as far as the least trusted committer of the
repository they are read from.

## Requirements

### Requirement: Task environment is an explicit allowlist

A task process across all runner modes SHALL receive only base shell variables, `CI_*` variables, resolved pipeline variables, and allowlisted `KESTRIX_RUNNER_ENV_PASSTHROUGH` variables. Critical host secrets (`KESTRIX_MASTER_KEY_PATH`, `KESTRIX_JWT_SIGNING_KEY_PATH`, `DATABASE_URL`, `KESTRIX_GITLAB_TOKEN`, `KESTRIX_WEBHOOK_SECRET`) SHALL NEVER reach tasks and fail startup if listed in passthrough.

#### Scenario: Control-plane secrets are withheld

- **WHEN** a task script prints its environment in `local` or `docker` mode
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

#### Scenario: Control-plane secrets never enter a remote job payload

- **WHEN** a task dispatches as a job for `remote` mode
- **THEN** the payload SHALL contain only the pipeline variables resolved for that task
- **AND** SHALL NOT contain `DATABASE_URL`, any `KESTRIX_*` control-plane variable, or any value from `KESTRIX_RUNNER_ENV_PASSTHROUGH`

### Requirement: Docker tasks run with reduced privilege and bounded resources

Tasks executed in `docker` mode SHALL drop all Linux capabilities, disable privilege escalation, and enforce PID, memory, and CPU bounds configured via `KESTRIX_RUNNER_DOCKER_*`. Docker containers SHALL attach to `KESTRIX_RUNNER_DOCKER_NETWORK` (default `bridge`) and SHALL NOT mount the Docker socket or run with `--privileged`.

#### Scenario: Default container hardening

- **WHEN** a task container is started with default configuration
- **THEN** the container SHALL have no capabilities, no new privileges, a PID limit, a memory limit, and a CPU limit
- **AND** `/var/run/docker.sock` SHALL NOT be mounted

#### Scenario: Network isolation opt-in

- **WHEN** `KESTRIX_RUNNER_DOCKER_NETWORK` is `none`
- **THEN** a task container SHALL have no network access
- **AND** a task that needs egress SHALL fail inside the container rather than reach the host network

#### Scenario: Approved manual task is hardened like a DAG task

- **WHEN** a manual task is approved in a project configured for `docker` mode
- **THEN** the task SHALL run in a container with the same capability, privilege, PID, memory, CPU, network, and user hardening as a `docker`-mode DAG task
- **AND** it SHALL NOT be executed with a bare host process, unlike a project configured for `local` mode

#### Scenario: Limit exceeded

- **WHEN** a task allocates more memory than the configured limit
- **THEN** the container SHALL be terminated by the runtime
- **AND** the task SHALL be recorded as `failed` with the termination in its output

### Requirement: Cancellation terminates task processes

Cancelling an in-process run (`local` or `docker`) SHALL terminate executing task processes or containers within `KESTRIX_RUNNER_CANCEL_GRACE` before marking the run `canceled`, remaining `running` if termination fails. Remote-mode runs SHALL transition to `canceled` immediately without reaching into remote worker machines.

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

#### Scenario: Cancel a remote-mode run with a task executing on a runner

- **WHEN** a `remote`-mode run is cancelled while one of its jobs is executing on a registered runner
- **THEN** the run SHALL be `canceled` with no termination attempted and no wait for the runner
- **AND** the cancel request SHALL NOT fail merely because that task's process is still executing remotely

### Requirement: Local execution requires an explicit opt-in

Kestrix SHALL execute a task in `local` mode inside its own process only when the instance setting `runner.allow_local` (environment variable `KESTRIX_RUNNER_ALLOW_LOCAL`) is true. The setting SHALL default to false and follow the configuration precedence rule. The instance runner mode itself (`runner.mode`) SHALL be settable via the environment variable `KESTRIX_EXECUTION_MODE`, following the same precedence rule; this is distinct from `KESTRIX_RUNNER_MODE`.

#### Scenario: Execution mode precedence
- **WHEN** `KESTRIX_EXECUTION_MODE` is provided
- **THEN** it SHALL configure the instance runner mode

### Requirement: Local execution rejection and warning rules

When `runner.allow_local` is false: startup SHALL fail if instance runner mode is `local` or unset, with a message stating that `local` tasks can read Kestrix's key files and naming `remote` and the opt-in as alternatives; creating or updating a project with mode `local` SHALL be rejected as validation error; a run of a stored `local` project SHALL fail before source acquisition. When true, startup SHALL log a warning naming the exposure and stored `local` projects count.
#### Scenario: Local instance mode without the opt-in
- **WHEN** Kestrix starts with `runner.mode` unset and `KESTRIX_RUNNER_ALLOW_LOCAL` unset
- **THEN** startup SHALL fail with a message naming the key-file exposure, `remote`, and `KESTRIX_RUNNER_ALLOW_LOCAL`

#### Scenario: Local project rejected without the opt-in
- **WHEN** the opt-in is off and a client creates a project with mode `local`
- **THEN** the request SHALL be rejected as a validation error

#### Scenario: Stored local project does not execute
- **WHEN** the opt-in is off and a run of a stored `local`-mode project is started
- **THEN** the run SHALL fail with the explanation
- **AND** no task script SHALL execute

#### Scenario: Opt-in accepted for a single-user machine
- **WHEN** Kestrix starts with `runner.mode: local` and `KESTRIX_RUNNER_ALLOW_LOCAL=true`
- **THEN** startup SHALL succeed and log a warning naming the exposure

### Requirement: Task timeout enforcement

When a task declares an execution timeout, the executor SHALL enforce that deadline. If execution exceeds the deadline, the executor SHALL terminate the task process or container and report the task result as `failed` with an error stating `task execution timed out after <duration>`.

#### Scenario: Local task timeout terminates process
- **WHEN** a local task exceeds its configured execution timeout
- **THEN** the executor SHALL terminate the task process group using SIGTERM followed by SIGKILL
- **AND** the task result SHALL be recorded as `failed` with a timeout error reason

#### Scenario: Docker task timeout stops container
- **WHEN** a containerized docker task exceeds its configured execution timeout
- **THEN** the executor SHALL stop the container with `docker stop`
- **AND** the task result SHALL be recorded as `failed` with a timeout error reason

#### Scenario: Remote task timeout terminates execution on runner
- **WHEN** a remote runner executes a task whose payload specifies a timeout and execution exceeds that duration
- **THEN** the runner SHALL terminate the task execution process
- **AND** report completion with status `failed` and error reason naming the elapsed timeout

### Requirement: Service-level local runner execution gate
The execution service layer (`RunService`) SHALL reject execution of any project or task resolving to `local` runner mode unless `runner.allow_local` is explicitly configured to `true`.

#### Scenario: Service rejects local run when allow_local is false
- **WHEN** `RunService.Start` is called on a project configured with runner mode `local`
- **AND** `runner.allow_local` is `false`
- **THEN** execution terminates immediately with `ErrLocalExecutionDisabled`
- **AND** no source repositories are acquired or cloned
