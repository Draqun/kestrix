## ADDED Requirements

### Requirement: Task artifacts

A `ci_task` block MAY declare `artifacts`, a list of paths relative to the
task's working directory that the task produces for tasks depending on it.
Each path SHALL be relative, SHALL NOT contain a `..` component, and SHALL
NOT be empty; a task SHALL declare at most 32. A pipeline violating any of
these SHALL fail to parse, naming the task and the path. In every execution
mode, a task whose script exits successfully but leaves a declared path
absent SHALL be recorded as failed with an error naming the missing path.

#### Scenario: Declare artifacts
- **WHEN** a pipeline declares `ci_task "build" { artifacts = ["dist/"] ... }`
- **THEN** the parsed task SHALL carry the artifact path `dist/`

#### Scenario: Escaping path rejected
- **WHEN** a task declares `artifacts = ["../secrets"]`
- **THEN** parsing SHALL fail naming the task and `../secrets`

#### Scenario: Missing artifact fails the task in local mode
- **WHEN** a `local`-mode task declaring `artifacts = ["dist/"]` exits 0 without creating `dist/`
- **THEN** the task SHALL be recorded as failed with an error naming `dist/`
