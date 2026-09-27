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
  url  = "https://gitlab.com/org/gateway.git"
  ref  = "main"
}

ci_repo "private_auth" {
  url           = "ssh://git@example.test/team/auth.git"
  ref           = "v2.1.0"
  credential_ref = "build-auth"
}
```

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | One of `path`/`url` | Relative subdirectory of the acquired primary project checkout |
| `url` | string | One of `path`/`url` | Standard HTTPS or SSH Git URL on any Git host; also makes the repository a webhook source of the project (see [Remote projects](remote-projects.md#sources)) |
| `ref` | string | Yes for remote repositories | Branch, tag, or commit SHA; the resolved SHA is recorded per run |
| `credential_ref` | string | No | Reference to runtime credentials; values never belong in HCL or URLs |
| `kind` | string | No | Retained for compatibility; `subpath` describes local monorepo paths |

`path` and `url` are mutually exclusive. A `path` must be relative, cannot
escape the primary checkout with `..`, and never names an arbitrary server
filesystem location. A `url` additionally wires the webhook: once the project
has loaded a pipeline declaring it, a push to that repository (with a webhook
pointing at Kestrix) marks the `ci_repo` as changed and evaluates the
project's triggers, with the pipeline loaded from the primary repository at
its configured ref. URLs containing passwords, tokens,
or other embedded credentials are rejected. Remote sources are cloned into a
private isolated workspace before the first task starts. Reusable bare Git
objects live in the private source cache, but writable task files never do.
Credential references
are resolved from Kestrix's encrypted credential store, which supports HTTPS
tokens, username/password or app passwords, and SSH private keys with optional
known-hosts data.

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
| `runner_tags` | list(string) | No | Tags a runner must carry to execute the task in `remote` mode; ignored in other modes (see [remote runners](remote-runners.md#runner-tags)) |

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

variable "REGISTRY_PASSWORD" {
  secret_ref = "cred_registry"
  scope      = "job"
  repo       = "gateway"
  job        = "publish"
}
```

| Attribute | Type | Description |
|-----------|------|-------------|
| `value` | string | Static value for the variable |
| `default` | string | Default value when not overridden |
| `secret_ref` | string | Reference to a `secret` credential; resolved at run start (see below) |
| `scope` | string | Visibility: `project`, `repo`, or `job` |
| `repo` | string | Required when `scope = "repo"` or `scope = "job"` |
| `job` | string | Required when `scope = "job"` |
| `env` | string | Environment variable name override |
| `protected` | bool | Only available on protected branches/tags |
| `masked` | bool | Hidden in job logs |

A variable supplies its value in exactly one of three ways: `value`,
`default`, or `secret_ref`. `secret_ref` cannot be combined with `value` or
`default` - declaring both fails validation and the pipeline does not start.

### Runtime secrets (`secret_ref`)

`secret_ref` names a `secret` credential in the encrypted credential store
(see [Repository credentials](repository-credentials.md)). A variable with
`secret_ref` is always masked, regardless of the `masked` attribute, and its
value is never written to Git: the HCL carries only the reference.

Kestrix resolves every `secret_ref` once, when a run starts or when an
approved manual task starts - never at any other time. A missing, revoked, or
non-`secret` reference fails the run before any task starts, naming the
variable and never the value; every task of that run is recorded `skipped`.
Approving a manual task re-resolves its secrets, so a task approved after a
secret was rotated receives the rotated value, not whatever was current when
the run started.

A resolved secret value of at least 8 bytes is redacted (replaced with `***`)
from run and task output before it is persisted or returned by any API. A
value shorter than 8 bytes is still injected into the task but is **not**
guaranteed to be redacted - avoid short runtime secrets in output-producing
tasks.

A literal `value` with `masked = true` is **not** a secret: `masked` on a
literal is accepted but is a no-op today. A plaintext value committed to Git
is never redacted from output and is visible to anyone who can read the
repository or the pipeline definition. Use `secret_ref` for anything that
must not appear in Git or in run history.

A `secret_ref` variable is resolved and injected as a normal pipeline
variable, subject to the same `scope`/`repo`/`job` rules as a literal one. It
is never forwarded to a dispatched GitLab pipeline.

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
- `openspec/specs/runtime-secrets/spec.md` — `secret_ref` resolution, injection, and redaction.
- `internal/parser/hcl_parser.go` — parser implementation.
- `ci/pipeline.tf` — example pipeline used by the project itself.
