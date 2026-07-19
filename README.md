# MetaCI

> Repo-agnostic CI/CD Control Plane with HCL-based DAG orchestration.

## What is MetaCI?

MetaCI decouples pipeline definitions from repositories. Instead of scattering
CI configuration across dozens of `.gitlab-ci.yml` files, you define your
entire system's pipeline in one or more HCL files — with support for
**OR/AND/NOT trigger logic** that is impossible in standard GitLab CI.

### The problem MetaCI solves

In multirepo setups, you have many services, many repos, and often an
integration-test repo. A single push to any service should trigger the right
subset of builds, integration tests, and deployments. Standard CI tools force
you into per-repo pipelines with no native way to express cross-repo
conditional triggers.

### Example

```hcl
project "my-system" {
  ci_repo "auth" {
    path = "src/auth"
  }
  ci_repo "gateway" {
    path = "src/gateway"
  }
  ci_repo "docs" {
    path = "docs"
  }

  # Integration tests run when EITHER service changes,
  # but NOT when only documentation changes.
  ci_task "integration_tests" {
    trigger    = "(repo.auth.changed || repo.gateway.changed) && !repo.docs.changed"
    depends_on = ["build_auth", "build_gateway"]

    execute {
      repo   = "tests"
      script = "./run.sh"
    }
  }
}
```

This expression `(A || B) && !C` has no equivalent in standard GitLab CI.

## Quick start

```bash
# Build
go build -o metaci ./cmd/metaci

# Validate a pipeline file (checks syntax, cycles, repo references)
./metaci validate examples/pipeline.tf

# Resolve which tasks would run given a set of changed repos
./metaci resolve --changed auth_service examples/pipeline.tf
./metaci resolve --changed auth_service,api_gateway examples/pipeline.tf
```

## Concepts

| Concept | Description |
|---------|-------------|
| **Project** | Top-level container grouping repos and tasks |
| **Repo** | A source repository (or subdirectory in monorepo) |
| **Task** | A unit of work with a trigger condition and execution spec |
| **Trigger** | A boolean expression using `repo.<name>.changed` predicates with `&&`, `\|\|`, `!` |
| **Execute** | The repo context and script to run (supports per-job container image) |
| **Manual** | Task requires human approval before execution |

## Architecture

```
┌──────────────┐     webhook     ┌────────────────┐     dispatch    ┌──────────────┐
│  Git Repo    │ ──────────────▶ │  MetaCI        │ ──────────────▶ │  GitLab      │
│  (push)      │                 │  Control Plane │                 │  Runner      │
└──────────────┘                 │                │                 └──────────────┘
                                 │  ┌──────────┐  │
                                 │  │ HCL      │  │
                                 │  │ Parser   │  │
                                 │  └──────────┘  │
                                 │  ┌──────────┐  │
                                 │  │ DAG      │  │
                                 │  │ Engine   │  │
                                 │  └──────────┘  │
                                 └────────────────┘
```

## Project structure

```
metaci/
├── cmd/metaci/          # CLI entry point
├── internal/
│   ├── model/           # Domain types (Project, Repo, Task)
│   ├── dag/             # DAG engine + expression parser
│   └── parser/          # HCL file parser
├── examples/            # Example pipeline.tf files
└── openspec/            # OpenSpec project configuration
```

## Status

**Proof of Concept** — core engine works. Webhook receiver and runner dispatch
are not yet implemented.

## License

All rights reserved. See [LICENSE](./LICENSE).
No usage without explicit permission from the copyright holder.
