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

## Install

Pull a released image from the GitLab Container Registry - `latest` tracks
the newest tagged release, or pin a specific one:

```bash
docker pull registry.gitlab.com/kestrix/kestrix:latest   # or a pinned tag, e.g. :v0.3.0
docker run --rm registry.gitlab.com/kestrix/kestrix:latest version
```

For a full production deployment (Docker Compose, TLS, backups, upgrades),
see [Production deployment](docs/deployment.md), which defaults to this same
registry image.

## Quick Start

The steps below are for running Kestrix from source instead of the published
image.

### 1. Build

```bash
make build
```

### 2. Provision what the server requires

`serve` refuses to start without PostgreSQL, a credential master key and a JWT
signing key, and every `/api` route except `/api/health`, the login route, and
first-run setup requires an account. Provision PostgreSQL and both keys once:

```bash
# PostgreSQL: starts the local dependencies (PostgreSQL) on the
# ports the default DATABASE_URL expects, :5433 among them. Run from the
# repository root; `docker-up` is a target of the root Makefile, and the one
# inside docker/ is called `deps-up-d`.
make docker-up

# Master key and signing key: 32 raw bytes (or 64 hex characters), mode 0600
mkdir -p ~/.kestrix && (umask 077 && \
  head -c 32 /dev/urandom > ~/.kestrix/master.key && \
  head -c 32 /dev/urandom > ~/.kestrix/jwt.key)
export KESTRIX_MASTER_KEY_PATH=~/.kestrix/master.key
export KESTRIX_JWT_SIGNING_KEY_PATH=~/.kestrix/jwt.key
```

Kestrix serves no request until an administrator account exists. The UI opens
directly on a "Create the first administrator" form when it detects a fresh
instance - just start the server (step 3) and visit it in a browser. For a
scripted deployment, create the account from the CLI instead, before or after
`serve` starts; either path produces the same account and closes the other:

```bash
# The password is read from the terminal, never a flag.
./bin/kestrix user create --role admin admin
```

### 3. Run

```bash
# Start the server (default: :8080); registered projects are remote-only
./bin/kestrix serve

# With dispatch to GitLab and webhook verification
./bin/kestrix serve \
  --dir ./examples/demo-mono \
  --dispatch 12345 \
  --gitlab-token glpat-xxx \
  --secret my-webhook-secret \
  --method gitlab

# From config file (~/.kestrix/config.yaml)
./bin/kestrix serve --config ~/.kestrix/config.yaml

# Serve the built UI alongside the API
./bin/kestrix serve --static ./ui/dist
```

### 4. Use

The CLI works on a pipeline file and needs no server:

```bash
# Resolve triggers
./bin/kestrix resolve examples/demo-mono/pipeline.tf --changed auth_service,billing_service

# Validate a pipeline
./bin/kestrix validate examples/demo-mono/pipeline.tf

# Run a pipeline locally in dependency order
./bin/kestrix run --changed kestrix ci/pipeline.tf

# Inspect resolved variables
./bin/kestrix variables ci/pipeline.tf --repo kestrix --env staging

# Manage accounts directly against the database
./bin/kestrix user list

# Print the running build's version - a released build reports its tag,
# e.g. v0.3.0; an unversioned local build reports dev
./bin/kestrix version
```

The API needs a bearer token from the login route:

```bash
# Health is the one open route
curl http://localhost:8080/api/health

TOKEN=$(curl -sS -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"name":"admin","password":"..."}' | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')

# Register a project. Substitute a repository you can actually reach: the
# URL below is a placeholder and the call will fail against it.
curl -X POST http://localhost:8080/api/projects \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"demo-mono","repo_url":"https://git.example.test/team/demo-mono.git","repo_ref":"main","pipeline_dir":"ci","pipeline_file":"pipeline.tf"}'

# Read it back, including the sources it now listens to
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/projects/demo-mono
```

Registration is not a metadata write: it validates `repo_url` by cloning it at
`repo_ref` and parsing the pipeline directory, so the remote has to be
reachable from the server. A private repository needs a credential created
first and referenced as `credential_ref`; see
[Repository credentials](docs/repository-credentials.md). An unreachable URL
answers `400` with the Git error, and the project is not created - the read
that follows would then answer `404`.

### 5. Deliver webhooks

Register a webhook in the Git host (GitLab → Settings → Webhooks) pointing at
`http://your-server:8080/webhook`, with the project's secret as the token.

Two things are easy to miss:

- **Verification fails closed.** A delivery is accepted only if it verifies
  against the matched project's own `webhook_secret` or the global
  `KESTRIX_WEBHOOK_SECRET`. With neither configured, every delivery is
  rejected with `401` and nothing is recorded.
- **Register a webhook on every source repository.** A project listens to its
  own repository *and* to each `ci_repo` in its pipeline that declares a
  `url`. A push to one of those secondary repositories triggers the project
  only if that repository also posts to `/webhook`.

See [Webhook receiver](docs/webhook-receiver.md) for resolution, fan-out and
the recorded history.

## Configuration

Kestrix loads config from `~/.kestrix/config.yaml` (auto-discovered).

```yaml
listen_addr: ":8080"
database_url: "postgres://kestrix:kestrix@localhost:5433/kestrix_local?sslmode=disable"
default_branch: "main"

pipeline:
  # Used by legacy pipeline endpoints; it does not auto-register a project.
  dir: "./examples/demo-mono"

webhook:
  secret: "your-webhook-secret"
  history_limit: 1000

dispatch:
  enabled: true
  project_id: 12345
  gitlab_url: "https://gitlab.com"
  gitlab_token: "glpat-xxx"

source_cache:
  directory: "/var/lib/kestrix/cache/git"
  max_age: "720h"
  max_bytes: 10737418240

runner:
  mode: "local"
  docker_pids: 256
  docker_memory: "2g"
  docker_cpus: "2"
  docker_user: ""
  docker_network: "bridge"
  cancel_grace: "10s"
  max_concurrent_runs: 2
  run_history_limit: 1000
  max_output_bytes: 10485760
```

**Precedence:** explicit CLI flag over config-file value over `KESTRIX_*`
environment over default. An explicitly empty file value counts as set and is
never filled from the environment. Unknown keys in `config.yaml` fail startup
naming the key. `webhook.method` has no file form: `--method` wins over
`KESTRIX_WEBHOOK_METHOD`.

Required to start:

| Env Variable | CLI Flag | Description |
|---|---|---|
| `DATABASE_URL` | `--database-url` | PostgreSQL connection URL (defaults to the local Compose instance on `:5433`) |
| `KESTRIX_MASTER_KEY_PATH` | — | Credential master key: 32 raw bytes or 64 hex characters, mode `0600` |
| `KESTRIX_JWT_SIGNING_KEY_PATH` | — | Token signing key, provisioned the same way |

Optional:

| Env Variable | CLI Flag | Description |
|---|---|---|
| `KESTRIX_ADDR` | `--addr` | Listen address (effective when the file omits `listen_addr`) |
| `KESTRIX_DIR` | `--dir` | Pipeline directory (effective when the file omits `pipeline.dir`) |
| `KESTRIX_GITLAB_URL` | `--gitlab-url` | GitLab base URL (effective when the file omits `dispatch.gitlab_url`) |
| `KESTRIX_WEBHOOK_SECRET` | `--secret` | Global webhook secret, used when the matched project has none of its own |
| `KESTRIX_WEBHOOK_METHOD` | `--method` | `gitlab` (X-Gitlab-Token comparison, default) or `hmac` (HMAC-SHA256 body signature) |
| `KESTRIX_WEBHOOK_HISTORY_LIMIT` | — | Retained webhook events (default `1000`) |
| `KESTRIX_RUNNER_MAX_CONCURRENT_RUNS` | — | Runs executed at once; runs beyond this are `queued` (default `2`, must be a positive integer) |
| `KESTRIX_RUN_HISTORY_LIMIT` | — | Retained terminal runs (default `1000`, must be a positive integer) |
| `KESTRIX_RUNNER_MAX_OUTPUT_BYTES` | — | Stored run output bound in bytes; output beyond it is truncated with a marker line (default `10485760`, must be a positive integer) |
| `KESTRIX_GITLAB_TOKEN` | `--gitlab-token` | GitLab API token |
| `KESTRIX_DISPATCH_PROJECT` | `--dispatch` | GitLab project ID (must be a positive integer; startup fails otherwise) |
| `KESTRIX_SOURCE_CACHE_DIR` | `--source-cache-dir` | Private bare-mirror cache directory |
| `KESTRIX_SOURCE_CACHE_MAX_AGE` | `--source-cache-max-age` | Maximum unused-entry age |
| `KESTRIX_SOURCE_CACHE_MAX_BYTES` | `--source-cache-max-bytes` | Maximum cache size in bytes |

Further settings cover HTTP timeouts and body bounds
(`KESTRIX_HTTP_*`, `KESTRIX_WEBHOOK_MAX_BODY_BYTES`), login throttling
(`KESTRIX_LOGIN_*`, `KESTRIX_TRUST_PROXY_HEADER`), the identity cache
(`KESTRIX_IDENTITY_CACHE_TTL`), the response CSP
(`KESTRIX_CONTENT_SECURITY_POLICY`) and the task runner
(`KESTRIX_RUNNER_*`). See `.env.template` for the complete list with
defaults.

`KESTRIX_*` is the only supported prefix for Kestrix-owned settings. Generic
variables such as `DATABASE_URL` keep their existing names. `DOCKER_*` are not
Kestrix settings: they reach task processes only as host variables named in
`KESTRIX_RUNNER_ENV_PASSTHROUGH`.

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
3. Kestrix matches the pushed repository against every project's sources and
   verifies the delivery against each matched project's secret
4. For each verified project it loads `pipeline.tf` from **that project's own
   repository** and resolves trigger conditions. A push to a secondary
   repository is never the source of the pipeline: the definition comes from
   the project's primary repository at its configured ref, and the pushed
   `ci_repo` is simply marked changed
5. If triggers match → dispatches a pipeline to GitLab via API, one per project
6. GitLab Runner picks up the job and executes

[Webhook receiver](docs/webhook-receiver.md) carries the full contract:
resolution order, fan-out, secondary-push semantics and the recorded history.

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
│   ├── auth/            # Password hashing, JWT issuing, identity cache
│   ├── config/          # YAML config + KESTRIX_* environment loading
│   ├── credentials/     # Encrypted credential store and master-key custody
│   ├── dag/             # DAG engine + trigger expression parser
│   ├── dispatch/        # GitLab pipeline dispatch client
│   ├── domain/          # Ports and shared types (no internal imports)
│   ├── mocks/           # Test doubles for the ports
│   ├── model/           # Pipeline model (Project, Task, Repo, Variable)
│   ├── parser/          # HCL parser (hclsyntax)
│   ├── runner/          # Local and Docker task execution
│   ├── service/         # Project, run and webhook services
│   ├── source/          # Git source acquisition and bare-mirror cache
│   ├── store/           # PostgreSQL project, credential, and run persistence
│   ├── variables/       # Variable resolution engine
│   ├── version/         # Build version reporting
│   └── web/             # HTTP server, webhook receiver, REST API
├── examples/            # One self-contained project per directory
│   ├── demo-mono/pipeline.tf
│   ├── simple-webservice/pipeline.tf
│   ├── multi-service/pipeline.tf
│   ├── enterprise-saas/pipeline.tf
│   └── remote-repository/pipeline.tf
├── ui/                  # React + MUI frontend
├── docs/                # Guides plus generated Swagger/OpenAPI
├── openspec/            # OpenSpec specs, changes and roadmap
├── docker/              # Compose stacks and images
└── Makefile             # Build system
```

## API Endpoints

Only `/api/health`, `/api/auth/login`, `/webhook` and `/docs/` are reachable
without a bearer token. The rest require an account, and the role column says
the minimum needed.

| Method | Path | Role | Description |
|---|---|---|---|
| GET | `/api/health` | — | Health check and stored counters |
| POST | `/api/auth/login` | — | Exchange name and password for a token |
| POST | `/webhook` | — | Git webhook receiver (verified by secret) |
| GET | `/docs/` | — | Swagger UI |
| GET | `/api/dashboard` | viewer | Aggregated dashboard counters |
| GET | `/api/version` | viewer | Build version |
| GET | `/api/pipeline`, `/api/pipeline/raw` | viewer | Parse or read HCL from the configured pipeline root |
| POST | `/api/resolve`, `/api/generate` | viewer | Resolve triggers, generate gitlab-ci.yml |
| GET | `/api/validate` | viewer | Validate a pipeline |
| GET | `/api/logs` | viewer | Webhook event history, paginated and filterable |
| GET | `/api/projects` | viewer | List projects |
| GET | `/api/projects/{name}` | viewer | Get a project, including its derived `sources` |
| GET | `/api/projects/{name}/definition` | viewer | Parsed pipeline from the project's repository |
| GET | `/api/projects/{name}/pipeline` | viewer | Raw project pipeline HCL |
| POST | `/api/projects/{name}/resolve` | viewer | Resolve a project's triggers |
| GET | `/api/projects/{name}/runs`, `/api/runs` | viewer | Run history |
| GET | `/api/projects/{name}/runs/{runID}` | viewer | One run with task results |
| POST | `/api/projects/{name}/run` | operator | Start a run |
| POST | `/api/projects/{name}/tasks/{task}/run` | operator | Run a single task |
| POST | `/api/projects/{name}/runs/{runID}/cancel` | operator | Cancel a running run |
| POST | `/api/projects/{name}/runs/{runID}/tasks/{task}/run` | operator | Approve a manual task |
| POST | `/api/projects/{name}/pipeline` | operator | Unsupported (405); registered pipeline HCL is Git-owned |
| POST/PUT/DELETE | `/api/projects`, `/api/projects/{name}` | admin | Create, update, delete a project |
| GET/POST | `/api/credentials` | admin | List and create credentials (metadata only in responses) |
| GET/PUT/POST | `/api/credentials/{ref}`, `/api/credentials/{ref}/revoke` | admin | Read, rotate, revoke |
| GET/POST/PUT/DELETE | `/api/users`, `/api/users/{user}/...` | admin | Accounts, roles and project grants |

## Example Pipelines

Each example is a directory holding one `pipeline.tf`:

| Directory | Scale | Description |
|---|---|---|
| `examples/demo-mono` | Small | Monorepo with subpath `ci_repo` blocks, used by the docs and tests |
| `examples/simple-webservice` | Small | Single webservice: lint → test → build → deploy |
| `examples/multi-service` | Medium | Microservices plus infra, with integration tests |
| `examples/enterprise-saas` | Large | Shared libraries, security and compliance stages |
| `examples/remote-repository` | Small | Remote `ci_repo` with `url` and `credential_ref` |

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

# Docker: dependencies only, or dependencies plus the Kestrix container
make docker-up
make docker-up-all

# UI development
cd ui && npm install && npm run dev
```

### Helper scripts

`scripts/` wraps the fetch/test/CI-poll sequences used across most sessions in this repo,
each starting with `set -euo pipefail` so a failing exit code is never hidden behind a pipe:

```bash
# Fetch, and confirm the working copy is on a bookmark and main isn't stale
scripts/preflight.sh

# Run every regression layer (or just the ones named), one PASS/FAIL line each
scripts/test-all.sh
scripts/test-all.sh unit coverage

# Poll the current branch's GitLab pipeline until it finishes; read-only,
# never merges or pushes
scripts/wait-green.sh
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

Every `/api` route requires a signed-in account. See
[Authentication](docs/authentication.md) for login, roles, project grants,
signing-key provisioning, and account recovery.

## License

Kestrix is licensed under the [PolyForm Noncommercial License 1.0.0](LICENSE):
personal projects, evaluation, study, and use by noncommercial organizations
are free. Commercial use requires a separate commercial license — see
[LICENSE-COMMERCIAL.md](LICENSE-COMMERCIAL.md) for the terms and
[PRICING.md](PRICING.md) for the plans. The software enforces no plan, account
limit, or license key.
