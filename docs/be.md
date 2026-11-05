# Backend

The Kestrix backend is a Go HTTP server that serves as the CI/CD control plane. It parses HCL pipeline definitions, resolves trigger expressions, dispatches jobs, and exposes a REST API.

## Core Packages

| Package | Responsibility |
|---------|---------------|
| `cmd/kestrix` | CLI entry point (serve, run, resolve, validate, runner, variables, user, keys, version) |
| `internal/auth` | Password hashing, JWT issuing, RBAC role enforcement, and identity caching |
| `internal/config` | Configuration loading (~/.kestrix/config.yaml + KESTRIX_* environment precedence) |
| `internal/credentials` | Master-key encryption custody and write-only Git credentials store |
| `internal/dag` | Trigger expression evaluation + task dependency ordering |
| `internal/dispatch` | GitLab CI pipeline dispatch client |
| `internal/domain` | Hexagonal architecture ports, models, and shared interfaces |
| `internal/model` | Shared pipeline, task, and project data types |
| `internal/parser` | HCL parsing (hclsyntax) → model.Project |
| `internal/runner` | Task execution engines: local bash, hardened Docker, and remote runners |
| `internal/service` | Project, run lifecycle, credential, and runner management services |
| `internal/source` | Remote Git repository acquisition, shallow cloning, and bare cache |
| `internal/store` | PostgreSQL project, run, credential, and runner persistence |
| `internal/variables` | Pipeline variable interpolation and resolution engine |
| `internal/web` | HTTP API (Chi router), SSE event stream, webhook handler, static UI |

## API Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/health` | Health check probe (unauthenticated) |
| POST | `/api/setup` | First-run administrator creation |
| POST | `/api/auth/login` | User authentication and JWT token issuing |
| GET | `/api/projects` | List projects (paginated) |
| POST | `/api/projects` | Register a new remote project |
| GET | `/api/projects/{name}/runs` | List runs for a project (paginated) |
| POST | `/api/projects/{name}/run` | Start a new pipeline run |
| GET | `/api/projects/{name}/runs/{runID}` | Get run status and task results |
| GET | `/api/projects/{name}/runs/{runID}/events` | Real-time SSE event stream for run and logs |
| POST | `/api/projects/{name}/runs/{runID}/cancel` | Cancel an in-flight pipeline run |
| GET | `/api/runners` | List registered remote runners |
| POST | `/api/runners` | Register a new runner token (admin only) |
| GET | `/api/credentials` | List repository credential metadata |
| POST | `/api/credentials` | Create write-only repository credential |
| POST | `/webhook` | Receive GitLab and GitHub push webhooks |
| GET | `/docs/` | Interactive Swagger UI & documentation |

## Configuration

Environment variables (`KESTRIX_*`) control runner mode, database URL, listen address, and more. Kestrix-owned settings use only this canonical prefix.
