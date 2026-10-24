# Spec: Pipeline Definition

## Purpose
The pipeline definition spec defines the structure and semantics of HCL pipeline files for Kestrix.

## Overview

The pipeline definition capability allows users to define CI/CD pipelines using HCL (HashiCorp Configuration Language). Pipelines are organized into projects, which group repositories and tasks.
## Requirements
### Requirement: Project Definition (implemented)

A pipeline SHALL be organized as a **project** — a named container that groups repositories and tasks. Each project SHALL have exactly one name and MAY have a default container image.

#### Scenario: Create a project

```
WHEN a user writes a project block in HCL
THEN the system SHALL parse it with a name and optional default_image attribute
AND the project name SHALL be used as the pipeline identifier
```

### Requirement: Repository Declaration (implemented)

A project SHALL declare one or more **ci_repo** blocks, each representing a
source repository or subdirectory in a monorepo. A local repository SHALL
define exactly one `path`. A remote repository SHALL define exactly one `url`
and SHALL define a non-empty `ref` identifying a branch, tag, or commit. A
repository MAY define `credential_ref` for private Git transport. Credential
values SHALL NOT be embedded in URLs or stored in HCL.

#### Scenario: Declare a monorepo subpath

```
WHEN a user writes ci_repo "auth" { path = "src/auth" type = "subpath" }
THEN the system SHALL register "auth" as a repository with path "src/auth"
AND the type "subpath" SHALL indicate a local directory within a monorepo
```

#### Scenario: Local monorepo subdirectory

- **WHEN** a `ci_repo` declaration contains `path = "services/api"`
- **THEN** Kestrix SHALL resolve the repository from the local project checkout
- **AND** SHALL use `services/api` as the repository working directory

#### Scenario: Remote repository declaration

- **WHEN** a `ci_repo` declaration contains `url = "ssh://git@example.test/team/auth.git"`, `ref = "v2.1.0"`, and `credential_ref = "build-auth"`
- **THEN** Kestrix SHALL acquire that Git source before executing dependent tasks
- **AND** SHALL use the requested tag
- **AND** SHALL expose only the logical repository name and non-secret source metadata to the run

#### Scenario: Invalid repository declaration

- **WHEN** a `ci_repo` declaration contains neither `path` nor `url`, or contains both
- **THEN** validation SHALL fail with an actionable declaration error
- **AND** Kestrix SHALL not start the pipeline

#### Scenario: Declare a remote repository

```
WHEN a user writes ci_repo "auth" { url = "ssh://git@example.test/team/auth.git" ref = "v2.1.0" }
THEN the system SHALL accept the declaration
AND the source acquisition SHALL use the requested ref
AND the run SHALL record the resolved commit SHA
```

#### Scenario: Missing remote ref

```
WHEN a remote ci_repo defines url but omits ref or sets it to an empty value
THEN validation SHALL fail with an actionable error
AND Kestrix SHALL not start the pipeline
```

#### Scenario: Embedded credentials

```
WHEN a repository URL contains a password, token, or unsupported userinfo
THEN validation SHALL reject the declaration
AND the error SHALL redact sensitive URL components
```

### Requirement: Task Declaration (implemented)

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

### Requirement: Trigger Expression Format (implemented)

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

### Requirement: Cross-Repo Execution (implemented)

A task's execute.repo SHALL reference a declared ci_repo. If the referenced repo is not declared, validation SHALL fail.

#### Scenario: Reference undeclared repo

```
WHEN a task references execute.repo = "nonexistent"
AND "nonexistent" is not declared as a ci_repo
THEN validation SHALL fail with an error
```

### REQ-1: Documentation inclusion (pending)
The pipeline definition documentation SHALL be included in the project‑docs site and shall be kept up‑to‑date with any behaviour changes.

#### Scenario: Documentation included in UI build
- **WHEN** the UI build runs,
- **THEN** the pipeline definition documentation shall be present in `ui/dist/docs`.

### Requirement: Variable Declaration

A project MAY declare **variable** blocks. Each `variable` block SHALL have
exactly one label, the variable key, and SHALL supply its value in exactly one
of three ways: a literal `value`, a literal `default`, or a `secret_ref`
naming a `secret` credential in the credential store. `secret_ref` SHALL NOT
be combined with `value` or `default`. A variable MAY set `scope` to
`project`, `repo`, or `job`; `repo` SHALL be required when `scope = "repo"`
and both `repo` and `job` SHALL be required when `scope = "job"`. A variable
MAY set `env`, `protected`, and `masked`. A variable with `secret_ref` SHALL
be masked regardless of the `masked` attribute. Secret values SHALL NOT be
written in HCL.

#### Scenario: Plaintext variable

- **WHEN** a user writes `variable "DOCKER_REGISTRY" { value = "registry.example.test" scope = "project" }`
- **THEN** the system SHALL parse it with the literal value and project scope

#### Scenario: Secret variable

- **WHEN** a user writes `variable "REGISTRY_PASSWORD" { secret_ref = "cred_abc" scope = "job" repo = "app" job = "publish" }`
- **THEN** the system SHALL parse it with the reference, job scope, and masked state
- **AND** the parsed definition SHALL contain no value for it

#### Scenario: Secret reference with a literal

- **WHEN** a `variable` block sets `secret_ref` and also `value` or `default`
- **THEN** validation SHALL fail with an error naming the variable and the conflicting attributes
- **AND** Kestrix SHALL not start the pipeline

#### Scenario: Scope without its target

- **WHEN** a `variable` block sets `scope = "job"` without `repo` and `job`
- **THEN** validation SHALL fail with an actionable declaration error

#### Scenario: Masked literal is documented as unmasked

- **WHEN** a `variable` block sets `masked = true` on a literal `value`
- **THEN** the system SHALL accept the declaration
- **AND** the documentation SHALL state that a literal value in Git is not a secret and is not redacted

### Requirement: Task runner tags

A `ci_task` block SHALL accept an optional `runner_tags` list of strings
naming the tags a runner must carry to execute the task in `remote` mode.
Tags SHALL be normalised (trimmed, lowercased, deduplicated) and SHALL match
`[a-z0-9._-]{1,64}`, at most 32 per task. Outside `remote` mode the attribute
SHALL be accepted and ignored.

#### Scenario: Declare runner tags
- **WHEN** a user writes `ci_task "train" { trigger = "true" runner_tags = ["GPU"] execute { repo = "ml" script = "./train.sh" } }`
- **THEN** the task SHALL be registered with runner tags `["gpu"]`

#### Scenario: Invalid runner tag rejected
- **WHEN** a `ci_task` declares a runner tag containing a space
- **THEN** parsing SHALL fail with an error naming the task and the tag

#### Scenario: Ignored outside remote mode
- **WHEN** a project in `docker` mode runs a task declaring `runner_tags`
- **THEN** the task SHALL execute as it would without the attribute

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

