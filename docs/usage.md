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
  -d '{"name":"my-app","repo_url":"https://git.example.test/team/my-app.git","repo_ref":"main","pipeline_dir":"ci","pipeline_file":"pipeline.tf","gitlab_project_id":12345}'
```

All registered projects require `repo_url` and `repo_ref`. For a private
remote, create a credential first and pass its non-secret
`repo_credential_ref`:

```bash
curl -X POST http://localhost:8080/api/projects \
  -H "Content-Type: application/json" \
  -d '{"name":"remote-app","repo_url":"https://git.example.test/team/app.git","repo_ref":"main","repo_credential_ref":"build-auth"}'
```

Credential values are created through the credential API/UI and stored
encrypted in PostgreSQL. The `repo_credential_ref` is only a lookup key;
plaintext values are never placed in project definitions, run history, or the
process environment.

The server validates remote access and parses the pipeline before persisting
the project. Registered pipeline content can be viewed in Kestrix but must be
edited and committed in Git. See [Remote projects](remote-projects.md).

## Docker deployment E2E test

Run the isolated production Compose smoke test with:

```bash
make docker-e2e
```

The test requires a running Docker daemon, Docker Compose, and `curl`. It builds
a local test image, uses a unique Compose project, high local ports, and
disposable PostgreSQL/Redis/cache volumes. It also registers a project from a
bare Git remote, verifies cache persistence across a Kestrix restart, proves a
cached credential fails closed after revocation, and scans API responses and
logs for the test secret. It removes only its own containers, network, image,
and volumes on success, failure, or interruption.

Set `KESTRIX_E2E_IMAGE` to use an existing local image instead of building one,
or override `KESTRIX_E2E_PORT` and `KESTRIX_E2E_TIMEOUT` when needed.
