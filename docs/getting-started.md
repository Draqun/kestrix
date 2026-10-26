# Getting Started with Kestrix

This guide walks you through setting up and using Kestrix from scratch. By the end of this guide, you will have a running Kestrix instance, an administrator account, a configured project with Git credentials, an automated HCL pipeline with DAG trigger resolution, webhook automation, and a remote runner.

---

## What is Kestrix?

**Kestrix** is a declarative, repository-agnostic CI/CD control plane. Instead of writing and maintaining dozens of vendor-locked `.gitlab-ci.yml` or GitHub Actions workflow files across multiple repositories:
- You write pipeline definitions in **HCL** (`pipeline.tf`).
- You declare repositories (`ci_repo`) and tasks (`ci_task`) with expressive boolean trigger logic (`repo.auth.changed || (repo.core.changed && !repo.docs.changed)`).
- Kestrix resolves the dependency graph (DAG), triggers only the tasks affected by a commit, and executes them in parallel across local processes, hardened Docker containers, or distributed remote runners.

```
┌─────────────────┐       Push Webhook       ┌─────────────────────────────────────┐
│ GitLab / GitHub │ ───────────────────────> │           Kestrix Server            │
└─────────────────┘                          │  - Evaluates changed files          │
                                             │  - Resolves DAG trigger expressions │
                                             │  - Dispatches tasks to runners      │
                                             └──────────────────┬──────────────────┘
                                                                │
                                      ┌─────────────────────────┴─────────────────────────┐
                                      ▼                                                   ▼
                         ┌─────────────────────────┐                         ┌─────────────────────────┐
                         │   Local/Docker Runner   │                         │      Remote Runner      │
                         │ (Runs on Kestrix host)  │                         │ (Outbound HTTPS poll)   │
                         └─────────────────────────┘                         └─────────────────────────┘
```

---

## 1. Quick Installation

### Option A: Docker Compose (Recommended for Production)

Clone the repository and start Kestrix using Docker Compose:

```bash
# Clone the repository
git clone https://gitlab.com/kestrix/kestrix.git
cd kestrix

# Generate encryption keys (AES-256 master key & JWT signing key)
mkdir -p ~/.kestrix
umask 077
head -c 32 /dev/urandom > ~/.kestrix/master.key
head -c 32 /dev/urandom > ~/.kestrix/jwt.key

# Start the stack (PostgreSQL + Kestrix Backend & UI)
export KESTRIX_MASTER_KEY_PATH=~/.kestrix/master.key
export KESTRIX_JWT_SIGNING_KEY_PATH=~/.kestrix/jwt.key
docker compose -f docker/docker-compose.prod.yml up -d
```

The Web UI will be available at **`http://localhost:18080`** (or port `:8080` in development).

### Option B: Pre-built Binary

Download the latest release binary for your platform from the GitLab Releases page:

```bash
# Download and make executable
curl -L -o kestrix https://gitlab.com/api/v4/projects/kestrix%2Fkestrix/packages/generic/kestrix/latest/kestrix
chmod +x kestrix
sudo mv kestrix /usr/local/bin/

# Verify installation
kestrix version
```

### Option C: From Source (Development)

```bash
# Prerequisites: Go 1.25+, Node.js 22+, Docker (optional)
git clone https://gitlab.com/kestrix/kestrix.git
cd kestrix

# Build binary
make build

# Start local PostgreSQL database container
make docker-up

# Provision master key and JWT signing key
mkdir -p ~/.kestrix
umask 077
head -c 32 /dev/urandom > ~/.kestrix/master.key
head -c 32 /dev/urandom > ~/.kestrix/jwt.key
export KESTRIX_MASTER_KEY_PATH=~/.kestrix/master.key
export KESTRIX_JWT_SIGNING_KEY_PATH=~/.kestrix/jwt.key

# Run dev server with hot reload
make dev-all
```

---

## 2. First-Run Administrator Setup

Kestrix enforces authentication for all API and management endpoints. On a fresh installation without existing users, Kestrix enters **setup mode**.

### Via Web UI
1. Navigate to `http://localhost:8080` (or your configured port).
2. The UI automatically displays the **"Welcome to Kestrix — Create Initial Administrator"** form.
3. Enter the administrator username and a secure password.
4. Click **Complete Setup** to initialize the database and log in.

### Via CLI
Alternatively, you can create the admin user directly from your terminal:

```bash
kestrix user create --role admin admin
# You will be prompted to enter and confirm the password securely
```

Once the initial admin is registered, the setup gate permanently closes and standard authentication takes effect.

---

## 3. Configuring Repository Credentials

If your project lives in a private GitLab, GitHub, or self-hosted Git repository, configure credentials before registering the project. Credentials are encrypted at rest using AES-256-GCM under your master key.

In the Web UI:
1. Navigate to **Settings → Credentials**.
2. Click **Add Credential**.
3. Choose the credential type:
   - **Personal Access Token / HTTPS Token**: Ideal for GitLab or GitHub access tokens.
   - **Username & Password**: For basic auth or application passwords.
   - **SSH Private Key**: For `git@gitlab.com:...` or `ssh://...` repositories (with optional `known_hosts` verification).
4. Give it a reference name (e.g., `gitlab-ci-token`).

Or via the REST API:
```bash
curl -X POST http://localhost:8080/api/credentials \
  -H "Authorization: Bearer <ADMIN_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "gitlab-ci-token",
    "type": "token",
    "secret": "glpat-xxxxxxxxxxxxxxxxxxxx"
  }'
```

---

## 4. Registering a Project

A Kestrix project links a remote Git repository and its pipeline definition.

### Via Web UI
1. Go to **Projects** and click **New Project**.
2. Fill in the project details:
   - **Name**: `my-app`
   - **Repository URL**: `https://gitlab.com/my-org/my-app.git` (or SSH: `git@gitlab.com:my-org/my-app.git`)
   - **Default Ref**: `main`
   - **Pipeline File**: `pipeline.tf` (or `ci/pipeline.tf`)
   - **Credential Ref**: Select `gitlab-ci-token` if private.
   - **Webhook Secret**: Enter an optional shared secret for HMAC signature verification.
3. Click **Create Project**. Kestrix validates connectivity and syntax before saving.

### Via REST API
```bash
curl -X POST http://localhost:8080/api/projects \
  -H "Authorization: Bearer <ADMIN_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-app",
    "repo_url": "https://gitlab.com/my-org/my-app.git",
    "repo_ref": "main",
    "pipeline_dir": ".",
    "pipeline_file": "pipeline.tf",
    "repo_credential_ref": "gitlab-ci-token",
    "webhook_secret": "whsec_supersecretkey123"
  }'
```

---

## 5. Writing Your Pipeline Definition (`pipeline.tf`)

Create a `pipeline.tf` file in the root of your project's Git repository.

Here is a complete, production-ready example illustrating repos, tasks, triggers, dependencies, variables, artifacts, and manual approval gates:

```hcl
project "my-app" {
  default_image = "golang:1.25-alpine"

  # Declare repositories that participate in change detection
  ci_repo "backend" {
    path = "src/backend"
    kind = "subpath"
  }

  ci_repo "frontend" {
    path = "src/frontend"
    kind = "subpath"
  }

  ci_repo "docs" {
    path = "docs"
    kind = "subpath"
  }

  # Task 1: Lint Backend (runs only when backend code changed)
  ci_task "lint_backend" {
    trigger = "repo.backend.changed"
    execute {
      repo   = "backend"
      script = "go vet ./... && gofmt -l ."
    }
  }

  # Task 2: Test Backend (depends on lint)
  ci_task "test_backend" {
    trigger    = "repo.backend.changed"
    depends_on = ["lint_backend"]
    execute {
      repo   = "backend"
      script = "go test -v ./..."
    }
  }

  # Task 3: Build Backend (produces binary artifact)
  ci_task "build_backend" {
    trigger    = "repo.backend.changed"
    depends_on = ["test_backend"]
    artifacts  = ["bin/server"]
    execute {
      repo   = "backend"
      script = "mkdir -p bin && go build -o bin/server ./cmd/server"
    }
  }

  # Task 4: Test Frontend (runs only when frontend files changed)
  ci_task "test_frontend" {
    trigger = "repo.frontend.changed"
    execute {
      repo   = "frontend"
      image  = "node:22-alpine"
      script = "npm ci && npm test"
    }
  }

  # Task 5: Build Frontend (produces dist/ artifact)
  ci_task "build_frontend" {
    trigger    = "repo.frontend.changed"
    depends_on = ["test_frontend"]
    artifacts  = ["dist/"]
    execute {
      repo   = "frontend"
      image  = "node:22-alpine"
      script = "npm ci && npm run build"
    }
  }

  # Task 6: Deploy Staging (runs when either backend or frontend changed)
  ci_task "deploy_staging" {
    trigger    = "repo.backend.changed || repo.frontend.changed"
    depends_on = ["build_backend", "build_frontend"]
    execute {
      script = "bash scripts/deploy-staging.sh"
    }
  }

  # Task 7: Deploy Production (requires manual approval gate)
  ci_task "deploy_production" {
    trigger    = "repo.backend.changed || repo.frontend.changed"
    depends_on = ["deploy_staging"]
    manual     = true
    execute {
      script = "bash scripts/deploy-production.sh"
    }
  }
}
```

### Validating Your Pipeline Locally
Before committing, you can validate your pipeline file using the Kestrix CLI:

```bash
# Validate syntax and check for dependency cycles
kestrix validate pipeline.tf

# Dry-run trigger evaluation for changes in backend
kestrix resolve --changed backend pipeline.tf
```

---

## 6. Automating Execution with Webhooks

Kestrix includes a built-in webhook receiver supporting both **GitLab** and **GitHub** push events.

### In GitLab:
1. Go to your repository **Settings → Webhooks**.
2. Set URL to: `http://<your-kestrix-host>:8080/webhook`
3. Set Secret Token to: your configured `webhook_secret`.
4. Select **Push events**.
5. Save webhook.

### In GitHub:
1. Go to repository **Settings → Webhooks → Add webhook**.
2. Set Payload URL: `http://<your-kestrix-host>:8080/webhook`
3. Set Content type: `application/json`.
4. Set Secret: your configured `webhook_secret`.
5. Select **Just the push event**.

### What happens on push:
1. Git host sends a webhook with the list of modified files.
2. Kestrix matches the repository to registered projects.
3. Kestrix classifies modified files, maps them to `ci_repo` blocks, and resolves the boolean trigger expressions.
4. Independent tasks start executing concurrently according to the DAG.
5. You can inspect all received webhooks under **Webhook Logs** in the Kestrix UI.

---

## 7. Monitoring & Managing Runs

Open the Kestrix Web UI at `http://localhost:8080` to observe your pipeline execution:

- **Dashboard**: High-level overview of recent pipeline runs, status distribution (Passed, Failed, Running, Queued, Manual), and duration metrics.
- **Pipelines**: Interactive list of runs with real-time status updates.
- **DAG Viewer**: Click any run to view the full dependency graph:
  - 🟢 **Passed**: Task completed successfully (exit code 0).
  - 🔴 **Failed**: Task failed with error logs.
  - 🟠 **Running**: Currently executing with live streaming logs.
  - 🔵 **Manual**: Paused waiting for an authorized operator to click **Approve**.
  - ⚪ **Skipped**: Dependency failed or was not triggered.
- **Live Output**: Click any task node to inspect its terminal stdout/stderr with ANSI color support.
- **Single-Task Rerun**: If a flaky test fails or an external network timeout occurs, click **Rerun Task** on that specific node without restarting the entire pipeline.

---

## 8. Setting Up Remote Runners

To execute build and test jobs on external worker machines instead of the Kestrix server:

### 1. Register the Runner
1. In the Web UI, go to **Settings → Runners → Register Runner**.
2. Provide a runner name (e.g. `worker-gpu-01`) and optional tags (e.g. `gpu`, `docker`, `linux`).
3. Click **Register**. Copy the generated runner token (displayed once).

### 2. Start the Runner on Worker Machine
On the target machine, run `kestrix runner`:

```bash
kestrix runner \
  --server https://kestrix.example.com \
  --token <YOUR_RUNNER_TOKEN> \
  --mode docker \
  --concurrency 4
```

The runner connects via outbound HTTPS long-polling. No open inbound ports or firewall configurations are required.

---

## 9. Next Steps

- [Pipeline Definition Guide](pipeline-definition.md) — Comprehensive HCL syntax, expressions, and variable interpolation.
- [DAG Engine](dag-engine.md) — Cycle detection, topological sorting, and trigger evaluation.
- [Remote Runners](remote-runners.md) — Tag matching, lease timeouts, and distributed job execution.
- [CLI Reference](cli-reference.md) — Full CLI flags and options cheat sheet.
- [Production Deployment](deployment.md) — Hardened production setups, TLS, and master key rotation.
