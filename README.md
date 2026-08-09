# Kestrix — CI/CD Control Plane

> Repo-agnostic CI/CD orchestrator with HCL-based DAG pipeline definitions.
> Supports OR/AND/NOT trigger logic for cross-repo conditional builds.

Project hosting and CI links are intentionally configured outside the Go
module identity. See `project.yaml` for the optional source location.

---

## What is Kestrix?

Kestrix is a CI/CD control plane that orchestrates pipelines across multiple
repositories using declarative HCL configuration. Instead of scattering CI
logic across dozens of `.gitlab-ci.yml` files, you define everything in one
place with clear trigger conditions.

**Key concepts:**
- **ci_repo** — maps a repository name to a path (monorepo) or Git URL
- **ci_task** — defines what to run, when to run it, and what it depends on
- **Trigger logic** — `repo.auth_service.changed`, `!repo.docs.changed`, `||`, `&&`

**Example trigger:**
```hcl
ci_task "deploy_staging" {
  trigger  = "repo.auth_service.changed || repo.billing_service.changed"
  repo     = "infra"
  script   = "terraform apply -auto-approve"
}
```

## Quick Start

### 1. Build

```bash
make build
```

### 2. Run

```bash
# Start the server (default: :8080); registered projects are remote-only
./bin/kestrix serve

# With dispatch to GitLab
./bin/kestrix serve \
  --dir ./examples/demo-mono \
  --dispatch 12345 \
  --gitlab-token glpat-xxx \
  --secret my-webhook-secret

# From config file (~/.kestrix/config.yaml)
./bin/kestrix serve --config ~/.kestrix/config.yaml

# Register a project explicitly
curl -X POST http://localhost:8080/api/projects \
  -H "Content-Type: application/json" \
  -d '{"name":"demo-mono","repo_url":"https://git.example.test/team/demo-mono.git","repo_ref":"main","pipeline_dir":"ci","pipeline_file":"pipeline.tf"}'
```

### 3. Use

```bash
# CLI: resolve triggers
./bin/kestrix resolve examples/demo-mono/pipeline.tf --changed auth_service,billing_service

# CLI: validate pipeline
./bin/kestrix validate examples/demo-mono/pipeline.tf

# CLI: run a pipeline locally in dependency order
./bin/kestrix run --changed kestrix ci/pipeline.tf

# API: health check
curl http://localhost:8080/api/health

# API: parse pipeline
curl http://localhost:8080/api/pipeline?dir=./examples/demo-mono

# API: resolve triggers
curl -X POST http://localhost:8080/api/resolve \
  -H "Content-Type: application/json" \
  -d '{"changed": ["auth_service"]}'

# Webhook: register in GitLab → Settings → Webhooks
# URL: http://your-server:8080/webhook
# Secret Token: your-webhook-secret
```

## Configuration

Kestrix loads config from `~/.kestrix/config.yaml` (auto-discovered).

```yaml
listen_addr: ":8080"
default_branch: "main"

pipeline:
  # Used by legacy pipeline endpoints; it does not auto-register a project.
  dir: "./examples/demo-mono"

webhook:
  secret: "your-webhook-secret"

dispatch:
  enabled: true
  project_id: 12345
  gitlab_url: "https://gitlab.com"
  gitlab_token: "glpat-xxx"

source_cache:
  directory: "/var/lib/kestrix/cache/git"
  max_age: "720h"
  max_bytes: 10737418240
```

**Precedence:** config file → env vars → CLI flags.

| Env Variable | CLI Flag | Description |
|---|---|---|
| `KESTRIX_WEBHOOK_SECRET` | `--secret` | Webhook HMAC secret |
| `KESTRIX_GITLAB_TOKEN` | `--gitlab-token` | GitLab API token |
| `KESTRIX_DISPATCH_PROJECT` | `--dispatch` | GitLab project ID |
| `KESTRIX_SOURCE_CACHE_DIR` | `--source-cache-dir` | Private bare-mirror cache directory |
| `KESTRIX_SOURCE_CACHE_MAX_AGE` | `--source-cache-max-age` | Maximum unused-entry age |
| `KESTRIX_SOURCE_CACHE_MAX_BYTES` | `--source-cache-max-bytes` | Maximum cache size in bytes |

`KESTRIX_*` is the only supported prefix for Kestrix-owned settings. Generic
variables such as `DATABASE_URL` and `DOCKER_*` keep their existing names.

## Project identity and repository location

Kestrix is the product name and `kestrix` is the provider-neutral Go module
path. The optional `source.url` field in `project.yaml` records where the
repository is hosted, but changing that URL does not require changing Go
imports. It is public metadata only; credentials belong in the encrypted
credential store and are referenced by `credential_ref`
variables.

Consumers that import internal packages must migrate imports from the former
provider-owned module path to `kestrix/...`. The old path is no longer the
project identity.

## Architecture

```
┌─────────────┐   push    ┌──────────────────┐   API    ┌───────────┐
│  Developer   │─────────▶│   GitLab         │────────▶│  Kestrix   │
└─────────────┘           │   (repo + CI)    │ webhook │  Server   │
                          └──────────────────┘◀────────│           │
                                    ▲                 │  ┌────────┤
                                    │ pipeline        │  │ REST   │
                                    └─────────────────│  │ + UI   │
                                         dispatch     │  │ + Web  │
                                                      └──┴────────┘
```

**Flow:**
1. Developer pushes code to GitLab
2. GitLab fires webhook to Kestrix (`/webhook`)
3. Kestrix parses `pipeline.tf`, resolves trigger conditions
4. If triggers match → dispatches pipeline to GitLab via API
5. GitLab Runner picks up the job and executes

**Components:**
- **Parser** — HCL → `model.Project` (repos, tasks, variables)
- **DAG Engine** — evaluates trigger expressions, resolves dependencies
- **File Classifier** — maps changed files → repository names
- **Dispatch Client** — GitLab Pipeline API integration
- **Web Server** — Chi router, REST API, webhook receiver, UI

## Project Structure

```
kestrix/
├── cmd/kestrix/          # CLI entry point
├── internal/
│   ├── config/          # YAML config loading (~/.kestrix/config.yaml)
│   ├── dag/             # DAG engine + trigger expression parser
│   ├── dispatch/        # GitLab pipeline dispatch client
│   ├── model/           # Data model (Project, Task, Repo, Variable)
│   ├── parser/          # HCL parser (hclsyntax)
│   ├── store/           # PostgreSQL project, credential, and run persistence
│   ├── variables/       # Variable resolution engine
│   └── web/             # HTTP server, webhook handler, API
├── examples/            # One self-contained project per directory
│   ├── demo-mono/pipeline.tf
│   ├── simple-webservice/pipeline.tf
│   ├── multi-service/pipeline.tf
│   └── enterprise-saas/pipeline.tf
├── ui/                  # React + MUI frontend
├── docs/                # Swagger/OpenAPI
├── openspec/            # OpenSpec documentation
├── docker/              # Docker setup
└── Makefile             # Build system
```

## API Endpoints

| Method | Path | Description |
|---|---|---|
| GET | `/api/health` | Health check |
| GET | `/api/pipeline` | Parse pipeline definition |
| GET | `/api/pipeline/raw` | Raw HCL source |
| POST | `/api/resolve` | Resolve trigger conditions |
| POST | `/api/generate` | Generate gitlab-ci.yml |
| GET | `/api/validate` | Validate pipeline |
| GET | `/api/projects` | List projects |
| POST | `/api/projects` | Create project |
| GET | `/api/projects/{name}` | Get project |
| DELETE | `/api/projects/{name}` | Delete project |
| GET | `/api/projects/{name}/pipeline` | Get project pipeline HCL |
| POST | `/api/projects/{name}/pipeline` | Unsupported (405); registered pipeline HCL is Git-owned |
| POST | `/api/projects/{name}/resolve` | Resolve project triggers |
| POST | `/webhook` | GitLab webhook receiver |
| GET | `/docs/` | Swagger UI |

## Example Pipelines

Three examples demonstrate different scales:

| File | Scale | Repos | Tasks | Description |
|---|---|---|---|---|
| `simple-webservice.tf` | Small | 1 | 5 | Single webservice: lint → test → build → deploy |
| `multi-service.tf` | Medium | 4 | 13 | 3 microservices + infra, integration tests |
| `enterprise-saas.tf` | Large | 13 | 32 | Enterprise SaaS, shared libs, security, compliance |

## Development

```bash
# Build
make build

# Run this repository's ci/pipeline.tf locally (same jobs as the configured CI)
make pipeline

# Test
make test

# Unit coverage (mandatory 100% gate; integration/e2e are excluded)
make test-coverage

# Separate regression layers
make test-integration
make test-e2e
make runner-unit-coverage

# Full quality gate
make quality

# Run dev server
make dev

# Build the UI and serve it with the API. Open http://localhost:8080.
make ui-serve DIR=.

# Docker
cd docker && make docker-up

# UI development
cd ui && npm install && npm run dev
```

### Running a pipeline from the UI

1. Start the application with `make ui-serve DIR=.`.
2. Push the repository to a reachable Git remote, then open **Projects** and
   register its URL, explicit ref, and pipeline directory. Create/select a
   credential first when the remote is private.
3. Select **Open Pipeline**, stay on the **Graph** view, enter `kestrix` in
   **Changed repos**, then choose **Run locally**.
4. The job output and final status appear below the graph. Manual tasks remain
   skipped unless **Auto-run manual jobs** is enabled.

Registered pipeline content is read-only in Kestrix. Edit and push HCL in Git;
each operation refreshes the remote and records the resolved commit SHA.

See [Remote projects](docs/remote-projects.md),
[Repository credentials](docs/repository-credentials.md), and the
[remote-only migration guide](docs/migrations/remote-only-projects.md) for the
complete onboarding, cache, security, and upgrade procedure.

## License

MIT
