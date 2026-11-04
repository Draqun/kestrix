# Spec Delta: task-execution-isolation

## ADDED Requirements

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
