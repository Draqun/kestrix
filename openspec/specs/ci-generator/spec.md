# Spec: CI Generator

## Overview

The CI generator produces a valid `.gitlab-ci.yml` from the resolved DAG nodes. It maps task dependencies to GitLab's `needs` keyword, sets per-job container images, and handles manual approval gates.

## Requirements

### Requirement: YAML Generation

The generator SHALL produce valid GitLab CI YAML from a list of resolved DAG nodes. The output SHALL include:
- A header comment identifying it as auto-generated
- Stage declarations derived from the dependency graph
- Default image from the project configuration

#### Scenario: Generate minimal pipeline

```
WHEN there is one task with no dependencies
THEN the output SHALL contain one stage "build"
AND the task SHALL be assigned to the "build" stage
```

### Requirement: Stage Derivation

Stages SHALL be derived from the dependency graph:
- Tasks with no dependencies → "build"
- Tasks depending on build tasks → "test"
- Tasks depending on test tasks → "deploy"

#### Scenario: Three-stage pipeline

```
WHEN tasks are: build_auth (no deps), test_integration (depends on build_auth), deploy (depends on test_integration)
THEN stages SHALL be: build, test, deploy
AND build_auth SHALL be in "build" stage
AND test_integration SHALL be in "test" stage
AND deploy SHALL be in "deploy" stage
```

### Requirement: Dependency Mapping

Task dependencies SHALL be mapped to GitLab's `needs` keyword. Each dependency SHALL be listed as a job name reference.

#### Scenario: Task with dependencies

```
WHEN a task has depends_on = ["build_a", "build_b"]
THEN the generated job SHALL include:
  needs:
    - build_a
    - build_b
```

### Requirement: Manual Approval

Tasks with `manual = true` SHALL include `when: manual` in the generated YAML, requiring human approval in the GitLab UI.

#### Scenario: Manual deploy job

```
WHEN a task has manual = true
THEN the generated job SHALL include "when: manual"
```

### Requirement: Per-Job Container Images

If a task specifies an image, the generated job SHALL include that image. If no image is specified, the project-level default SHALL be used.

#### Scenario: Custom image per job

```
WHEN a task has execute.image = "rust:1.75"
THEN the generated job SHALL include "image: rust:1.75"
```

### Requirement: Script Generation

Each job SHALL include a script section that:
1. Logs the task name and repo context
2. Executes the task's script command

#### Scenario: Script output

```
WHEN a task has script = "./build.sh"
THEN the generated job SHALL include:
  script:
    - echo "Running <task_name> in repo <repo>"
    - ./build.sh
```
