# Usage Guide

## Running the server locally
```bash
make dev   # starts the backend with hot‑reload
make ui-dev   # starts the frontend dev server (http://localhost:3000)
```

Projects are managed explicitly through the project API or UI. Starting the
server with a pipeline directory does not create a project automatically.

## Database profiles

Local development uses PostgreSQL database `kestrix_local` on host port `5433`
and a dedicated Docker volume. The host-run server falls back to the same URL:
`postgres://kestrix:kestrix@localhost:5433/kestrix_local`.

Deployment uses the explicit `DATABASE_URL` from
`docker/docker-compose.prod.yml`, database `kestrix`, and the deployment
PostgreSQL volume. The profiles are intentionally isolated. Data from the old
shared database is not copied or deleted automatically; export/import it
explicitly if it must be recovered.

## Building the UI and documentation
```bash
make ui-build   # builds UI and copies Markdown docs to ui/dist/docs
```

## Executing a pipeline
```bash
make pipeline   # runs the example pipeline in `examples/demo-mono`
```

## Adding a new project
```bash
curl -X POST http://localhost:8080/api/projects \
  -H "Content-Type: application/json" \
  -d '{"name":"my-app","repo_path":"./my-app","gitlab_project_id":12345}'
```
