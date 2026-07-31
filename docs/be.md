# Backend

The Kestrix backend is a Go HTTP server that serves as the CI/CD control plane. It parses HCL pipeline definitions, resolves trigger expressions, dispatches jobs, and exposes a REST API.

## Core Packages

| Package | Responsibility |
|---------|---------------|
| `cmd/kestrix` | CLI entry point (serve, run, resolve, validate) |
| `internal/parser` | HCL → `model.Project` |
| `internal/dag` | Trigger expression evaluation + task ordering |
| `internal/dispatch` | GitLab CI pipeline dispatch |
| `internal/runner` | Local/Docker/GitLab‑Runner task execution |
| `internal/web` | HTTP API (Chi router), webhook handler |
| `internal/store` | PostgreSQL project and run persistence |
| `internal/model` | Shared data types |

## API Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/projects` | List projects (paginated) |
| POST | `/api/projects` | Create a project |
| GET | `/api/projects/{name}/runs` | List runs for a project (paginated) |
| POST | `/api/projects/{name}/run` | Start a new pipeline run |
| POST | `/webhook` | Receive GitLab push events |
| GET | `/docs/` | Swagger UI & documentation |

## Configuration

Environment variables (`KESTRIX_*`) control runner mode, database URL, listen address, and more. Kestrix-owned settings use only this canonical prefix.
