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

## For Developers

### Prerequisites

- Go 1.26+
- Make
- Git

### Getting started

```bash
git clone https://gitlab.com/lunaris-bot/metaci.git
cd metaci
make init          # install deps + build binary
```

### Available commands

```bash
make help          # show all available targets
make build         # build the metaci binary
make test          # run all tests (27+)
make quality       # full quality gate: fmt + vet + test + coverage
make validate      # validate examples/pipeline.tf
make resolve CHANGED=auth_service   # simulate webhook
make serve ADDR=:8080 DIR=./examples  # start webhook server
make clean         # remove build artifacts
```

### Validate a pipeline

```bash
# Check syntax, cycles, and repo references
./bin/metaci validate examples/pipeline.tf
```

### Resolve triggers

```bash
# Which tasks run when auth_service changes?
./bin/metaci resolve --changed auth_service examples/pipeline.tf

# Multiple repos changed
./bin/metaci resolve --changed auth_service,api_gateway examples/pipeline.tf

# Docs-only change — should trigger nothing
./bin/metaci resolve --changed docs examples/pipeline.tf
```

### Run the webhook server

```bash
# Start server on port 8080, serving pipeline configs from ./examples
./bin/metaci serve --addr :8080 --dir ./examples

# Test with a simulated GitLab push event
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "object_kind": "push",
    "ref": "refs/heads/main",
    "project": {
      "path_with_namespace": "src/auth",
      "default_branch": "main"
    },
    "commits": [{
      "id": "abc123",
      "message": "fix handler",
      "added": [],
      "modified": ["handler.go"],
      "removed": []
    }]
  }'

# Health check
curl http://localhost:8080/health
```

### Test coverage

```bash
make quality       # includes coverage report
# Reports saved to coverage/coverage.html
```

Current coverage:
- `internal/dag`:     82.4%
- `internal/parser`:  80.6%
- `internal/web`:     55.7%
- Total:              61.1%

### Project structure

```
metaci/
├── cmd/metaci/              # CLI entry point (resolve, validate, serve)
├── internal/
│   ├── model/               # Domain types (Project, Repo, Task, Variable)
│   ├── dag/                 # DAG engine + boolean expression parser
│   ├── parser/              # HCL file parser (manual hclsyntax)
│   ├── variables/           # GitLab-like variable store (scoped)
│   └── web/                 # Webhook receiver + CI YAML generator
├── examples/                # Example pipeline.tf files
├── openspec/                # OpenSpec project docs (proposal, specs, design)
├── .golangci.yml            # Linter configuration
├── Makefile                 # Build system (make help for targets)
└── go.mod
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
| **Variable** | Scoped config values: project → repo → job (most specific wins) |

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
                                 │  ┌──────────┐  │
                                 │  │ Webhook  │  │
                                 │  │ Receiver │  │
                                 │  └──────────┘  │
                                 │  ┌──────────┐  │
                                 │  │ Variables│  │
                                 │  │ Store    │  │
                                 │  └──────────┘  │
                                 └────────────────┘
```

## License

All rights reserved. See [LICENSE](./LICENSE).
No usage without explicit permission from the copyright holder.
