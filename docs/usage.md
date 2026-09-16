# Usage Guide

## Running the server locally
```bash
make dev   # starts the backend with hot‑reload
make ui-dev   # starts the frontend dev server (http://localhost:3000)
```

Projects are managed explicitly through the project API or UI. Starting the
server with a pipeline directory does not create a project automatically.

Before starting a host-run server, provide its separately managed credential
master-key file and JWT signing-key file (each mode `0600`, and never the
same file as each other - see [Authentication](authentication.md) for why):

```bash
export KESTRIX_MASTER_KEY_PATH=/home/you/.kestrix/master.key
export KESTRIX_JWT_SIGNING_KEY_PATH=/home/you/.kestrix/jwt.key
make dev
```

Alternatively, put both paths in `.env`; `make dev` and `make dev-all` load the
file automatically.

For Compose, use the same absolute paths; the service mounts each read-only
at that path inside the container.

Kestrix refuses every `/api` request until an administrator account exists.
Create the first one once the server can reach the database:

```bash
kestrix user create --role admin <name>
```

See [Authentication](authentication.md) for login, roles, project grants,
account recovery, revocation latency, login throttling, and the security
response headers. All of that section's environment variables beyond the two
key paths above are optional and default to sane values.

## Database profiles

Local development uses PostgreSQL database `kestrix_local` on host port `5433`
and a dedicated Docker volume. The host-run server falls back to the same URL:
`postgres://kestrix:kestrix@localhost:5433/kestrix_local`.

Deployment uses the explicit `DATABASE_URL` from
`docker/docker-compose.prod.yml`, database `kestrix`, and the deployment
PostgreSQL volume. The profiles are intentionally isolated. Data from the old
shared database is not copied or deleted automatically; export/import it
explicitly if it must be recovered.

## Run concurrency, retention, and output bounds

Kestrix bounds how much work one instance executes at once and how much run
history and output it keeps:

- `KESTRIX_RUNNER_MAX_CONCURRENT_RUNS` (default `2`) caps runs executing at
  once. A run triggered while every slot is busy is recorded `queued` and
  starts automatically, in acceptance order, as slots free. Cancelling a
  `queued` run marks it `canceled` without executing anything.
- `KESTRIX_RUN_HISTORY_LIMIT` (default `1000`) caps retained runs. Once a run
  finishes, the oldest terminal (`passed`/`failed`/`canceled`) runs beyond the
  limit are deleted; a `running`, `queued`, or manual-approval-pending run is
  never deleted by retention.
- `KESTRIX_RUNNER_MAX_OUTPUT_BYTES` (default `10485760`, 10 MiB) caps stored
  run output. Output beyond the bound is dropped and the stored output ends
  with a marker line naming the bound; the run's status is unaffected.

All three must be positive integers; an invalid value fails startup naming
the setting. Upgrading an existing deployment applies retention starting from
its first run to finish after the upgrade, which may prune a backlog of old
runs down to the configured limit immediately.

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

All registered projects require `repo_url` and `repo_ref`. `repo_url` must be
an `https://` URL, an `ssh://` URL, or an scp-like `user@host:path` address;
other schemes (plaintext `http://`, `file://`, `git://`, transport helpers
like `ext::`) and a literal loopback, link-local, or unspecified host are
rejected with `400` before any network access. An optional `webhook_secret`
on create or update is write-only: it is never returned by the project API,
which instead reports `has_webhook_secret`; an update that omits it (or sends
it empty) keeps the stored secret unchanged. For a private remote, create a
credential first and pass its non-secret `repo_credential_ref`:

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

## Cutting a release

`scripts/release.sh <X.Y.Z>` is the only supported way to cut a release. Run
it on a clean `main`:

```bash
scripts/release.sh 0.3.0
```

It refuses to run on a dirty working copy or off `main`, moves the
`CHANGELOG.md` `[Unreleased]` section under a new `## [vX.Y.Z] - YYYY-MM-DD`
heading (leaving a fresh empty `[Unreleased]`), commits
`chore: release vX.Y.Z`, tags it, and pushes the commit and tag. Pushing the
tag triggers the `release` stage in `.gitlab-ci.yml`, which only runs once
every analysis/test/build job for that commit has passed:

- `publish-image` builds and pushes `registry.gitlab.com/kestrix/kestrix:vX.Y.Z`
  and `:latest`.
- `release-binary` builds the stripped Linux amd64 binary and uploads it to
  the project's generic package registry.
- `create-release` creates a GitLab Release for the tag, with the matching
  `CHANGELOG.md` section as its description and the binary linked as an
  asset.

Watch it with `scripts/wait-green.sh vX.Y.Z`, then verify from outside the
runner: `docker pull registry.gitlab.com/kestrix/kestrix:vX.Y.Z` and
`docker run --rm registry.gitlab.com/kestrix/kestrix:vX.Y.Z version` should
both report the tag, and the project's Releases page should list it.

**Rollback:** a tag pushed by mistake is not silently fixed - delete it by
hand. Delete the GitLab Release and the `vX.Y.Z` tag from the project (UI or
`glab release delete vX.Y.Z` / `git push origin :refs/tags/vX.Y.Z`), which
also removes the published Release notes; the pushed image and package
registry asset are left in the registry and can be deleted separately if
needed. A deployment already pinned to that tag is unaffected until it next
pulls. See [Production deployment](deployment.md#10-rollback) for rolling a
running deployment back to a previous tag.

## Docker deployment E2E test

Run the isolated production Compose smoke test with:

```bash
make docker-e2e
```

The test requires a running Docker daemon, Docker Compose, and `curl`. It builds
a local test image, uses a unique Compose project, high local ports, and
disposable PostgreSQL/cache volumes. It also registers a project from a
bare Git remote, verifies cache persistence across a Kestrix restart, proves a
cached credential fails closed after revocation, and scans API responses and
logs for the test secret. It removes only its own containers, network, image,
and volumes on success, failure, or interruption.

Set `KESTRIX_E2E_IMAGE` to use an existing local image instead of building one,
or override `KESTRIX_E2E_PORT` and `KESTRIX_E2E_TIMEOUT` when needed.
