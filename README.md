# MetaCI — CI/CD Control Plane

> Repo-agnostic CI/CD orchestrator with HCL-based DAG pipeline definitions.
> Supports OR/AND/NOT trigger logic for cross-repo conditional builds.

[![pipeline status](https://gitlab.com/lunaris-bot/metaci/badges/main/pipeline.svg)](https://gitlab.com/lunaris-bot/metaci/-/pipelines)
[![coverage report](https://gitlab.com/lunaris-bot/metaci/badges/main/coverage.svg)](https://gitlab.com/lunaris-bot/metaci/-/pipelines)

---

## What is MetaCI?

MetaCI is a CI/CD control plane that orchestrates pipelines across multiple
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
# Serve with examples (default: :8080)
./bin/metaci serve --dir ./examples

# With dispatch to GitLab
./bin/metaci serve \
  --dir ./examples \
  --dispatch 12345 \
  --gitlab-token glpat-xxx \
  --secret my-webhook-secret

# From config file (~/.metaci/config.yaml)
./bin/metaci serve --config ~/.metaci/config.yaml
```

### 3. Use

```bash
# CLI: resolve triggers
./bin/metaci resolve examples/pipeline.tf --changed auth_service,billing_service

# CLI: validate pipeline
./bin/metaci validate examples/pipeline.tf

# API: health check
curl http://localhost:8080/api/health

# API: parse pipeline
curl http://localhost:8080/api/pipeline?dir=./examples

# API: resolve triggers
curl -X POST http://localhost:8080/api/resolve \
  -H "Content-Type: application/json" \
  -d '{"changed": ["auth_service"]}'

# Webhook: register in GitLab → Settings → Webhooks
# URL: http://your-server:8080/webhook
# Secret Token: your-webhook-secret
```

## Configuration

MetaCI loads config from `~/.metaci/config.yaml` (auto-discovered).

```yaml
listen_addr: ":8080"
default_branch: "main"

pipeline:
  dir: "./examples"

webhook:
  secret: "your-webhook-secret"

dispatch:
  enabled: true
  project_id: 12345
  gitlab_url: "https://gitlab.com"
  gitlab_token: "glpat-xxx"
```

**Precedence:** config file → env vars → CLI flags.

| Env Variable | CLI Flag | Description |
|---|---|---|
| `METACI_WEBHOOK_SECRET` | `--secret` | Webhook HMAC secret |
| `METACI_GITLAB_TOKEN` | `--gitlab-token` | GitLab API token |
| `METACI_DISPATCH_PROJECT` | `--dispatch` | GitLab project ID |

## Architecture

```
┌─────────────┐   push    ┌──────────────────┐   API    ┌───────────┐
│  Developer   │─────────▶│   GitLab         │────────▶│  MetaCI   │
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
2. GitLab fires webhook to MetaCI (`/webhook`)
3. MetaCI parses `pipeline.tf`, resolves trigger conditions
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
metaci/
├── cmd/metaci/          # CLI entry point
├── internal/
│   ├── config/          # YAML config loading (~/.metaci/config.yaml)
│   ├── dag/             # DAG engine + trigger expression parser
│   ├── dispatch/        # GitLab pipeline dispatch client
│   ├── model/           # Data model (Project, Task, Repo, Variable)
│   ├── parser/          # HCL parser (hclsyntax)
│   ├── store/           # Persistent project config (JSON file)
│   ├── variables/       # Variable resolution engine
│   └── web/             # HTTP server, webhook handler, API
├── examples/            # Example pipeline definitions
│   ├── pipeline.tf      # Original demo-mono
│   ├── simple-webservice.tf
│   ├── multi-service.tf
│   └── enterprise-saas.tf
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
| POST | `/api/projects/{name}/pipeline` | Save project pipeline HCL |
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

# Test
make test

# Full quality gate
make quality

# Run dev server
make dev

# Docker
cd docker && make docker-up

# UI development
cd ui && npm install && npm run dev
```

## License

MIT
