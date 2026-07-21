# Pipeline Definition

Kestrix pipelines are defined using HCL (HashiCorp Configuration Language). Each pipeline file uses the `.tf` extension and contains a single `project` block that groups repositories, tasks, and variables.

## Project Block

A pipeline starts with a `project` block. The project name is the label and serves as the pipeline identifier.

```hcl
project "my-service" {
  default_image = "golang:1.22"

  # ci_repo, ci_task, and variable blocks go here
}
```

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `default_image` | string | No | Fallback container image for tasks without an explicit image |

## Repositories (`ci_repo`)

Repositories are the units of change detection. Each `ci_repo` block declares a repository that can trigger tasks and host scripts.

```hcl
ci_repo "auth" {
  path = "src/auth"
  kind = "subpath"
}

ci_repo "gateway" {
  path = "https://gitlab.com/org/gateway.git"
  kind = "git"
}
```

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | Yes | Clone URL (multi-repo) or subdirectory (monorepo) |
| `kind` | string | No | `"subpath"` for monorepo subdirectory, `"git"` for remote (default) |

## Tasks (`ci_task`)

Tasks are units of work. Each task has a trigger condition that decides whether it should run, an execution specification, and optional dependency declarations.

```hcl
ci_task "build" {
  trigger    = "repo.auth.changed"
  depends_on = ["lint"]
  manual     = false

  execute {
    repo   = "auth"
    script = "bash scripts/build.sh"
    image  = "golang:1.22"
  }
}
```

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `trigger` | string | Yes | Boolean expression deciding if the task runs |
| `depends_on` | list(string) | No | Tasks that must complete before this one starts |
| `manual` | bool | No | If `true`, requires human approval before execution |

### Execute Block

The `execute` block inside a task specifies what to run and where:

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `repo` | string | Yes | References a declared `ci_repo` by name |
| `script` | string | Yes | Command to run in the repo's working directory |
| `image` | string | No | Per-job container image override |

## Trigger Expressions

Trigger expressions are boolean formulas that determine whether a task runs. They reference repository change states.

### Syntax

| Element | Meaning |
|---------|---------|
| `repo.<name>.changed` | True when repository `<name>` has changes |
| `&&` | Logical AND |
| `\|\|` | Logical OR |
| `!` | Logical NOT |
| `(...)` | Grouping for precedence |

### Examples

```hcl
trigger = "repo.auth.changed"
trigger = "repo.auth.changed && repo.gateway.changed"
trigger = "repo.auth.changed || repo.gateway.changed"
trigger = "!repo.docs.changed"
trigger = "(repo.auth.changed || repo.gateway.changed) && !repo.docs.changed"
```

If a task has no trigger expression, it runs by default (always triggered).

## Variables

Pipelines can declare typed variables with scope controls:

```hcl
variable "DOCKER_REGISTRY" {
  value   = "registry.gitlab.com"
  scope   = "project"
  masked  = true
}

variable "DEPLOY_TARGET" {
  default = "staging"
  scope   = "repo"
  repo    = "gateway"
}
```

| Attribute | Type | Description |
|-----------|------|-------------|
| `value` | string | Static value for the variable |
| `default` | string | Default value when not overridden |
| `scope` | string | Visibility: `project`, `repo`, or `job` |
| `repo` | string | Required when `scope = "repo"` |
| `job` | string | Required when `scope = "job"` |
| `env` | string | Environment variable name override |
| `protected` | bool | Only available on protected branches/tags |
| `masked` | bool | Hidden in job logs |

## Multi-File Pipelines

A pipeline directory can contain multiple `.tf` files. The parser reads all of them and merges:

- All `ci_repo` blocks are collected.
- All `ci_task` blocks are collected.
- All `variable` blocks are collected.
- Project name and `default_image` are taken from the first file that sets them. If two files declare different project names, parsing fails.

This enables splitting large pipelines into focused files:

```
ci/pipeline.tf          # project block + core tasks
ci/deploy.tf            # deployment tasks
ci/variables.tf         # variable declarations
```

## Validation

The parser validates pipeline files at parse time:

- Unknown top-level blocks are rejected.
- Unknown child blocks inside `project` are rejected.
- Interpolated strings (`"${var.name}"`) are not supported — only literal values.
- Object literals are not supported.
- Variable references and scoped traversals are not supported.
- Cross-file project name mismatches are rejected.

## Further Reading

- `openspec/specs/pipeline-definition/spec.md` — full specification and test scenarios.
- `internal/parser/hcl_parser.go` — parser implementation.
- `ci/pipeline.tf` — example pipeline used by the project itself.
