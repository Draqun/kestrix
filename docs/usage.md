# Usage Guide

## Running the server locally
```bash
make dev   # starts the backend with hot‑reload
make ui-dev   # starts the frontend dev server (http://localhost:3000)
```

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
