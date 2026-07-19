# Spec: Pipeline Definition

## Overview

The pipeline definition capability allows users to define CI/CD pipelines using HCL (HashiCorp Configuration Language). Pipelines are organized into projects, which group repositories and tasks.

## Requirements

### Requirement: Project Definition

A pipeline SHALL be organized as a **project** — a named container that groups repositories and tasks. Each project SHALL have exactly one name and MAY have a default container image.

#### Scenario: Create a project

```
WHEN a user writes a project block in HCL
THEN the system SHALL parse it with a name and optional default_image attribute
AND the project name SHALL be used as the pipeline identifier
```

### Requirement: Repository Declaration

A project SHALL declare one or more **ci_repo** blocks, each representing a source repository (or subdirectory in a monorepo). Each repo SHALL have a name (label) and a path.

#### Scenario: Declare a monorepo subpath

```
WHEN a user writes ci_repo "auth" { path = "src/auth" type = "subpath" }
THEN the system SHALL register "auth" as a repository with path "src/auth"
AND the type "subpath" SHALL indicate a local directory within a monorepo
```

### Requirement: Task Declaration

A project SHALL declare one or more **ci_task** blocks. Each task SHALL have:
- A name (label)
- A trigger expression (string)
- An execute block with repo, script, and optional image
- Optional depends_on list
- Optional manual flag

#### Scenario: Declare a build task

```
WHEN a user writes ci_task "build" { trigger = "repo.auth.changed" execute { repo = "auth" script = "./build.sh" } }
THEN the system SHALL register a task named "build"
AND the trigger expression SHALL be stored for later evaluation
AND the execute block SHALL reference the "auth" repository
```

### Requirement: Trigger Expression Format

Trigger expressions SHALL be string literals using the following syntax:
- Predicates: `repo.<name>.changed`
- Operators: `&&` (AND), `||` (OR), `!` (NOT)
- Grouping: parentheses for precedence
- All expressions SHALL be quoted strings in HCL

#### Scenario: Complex trigger expression

```
WHEN a user writes trigger = "(repo.auth.changed || repo.gateway.changed) && !repo.docs.changed"
THEN the system SHALL parse the expression as a boolean formula
AND the expression SHALL be evaluatable against a set of changed repositories
```

### Requirement: Cross-Repo Execution

A task's execute.repo SHALL reference a declared ci_repo. If the referenced repo is not declared, validation SHALL fail.

#### Scenario: Reference undeclared repo

```
WHEN a task references execute.repo = "nonexistent"
AND "nonexistent" is not declared as a ci_repo
THEN validation SHALL fail with an error
```
