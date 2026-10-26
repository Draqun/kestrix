# CLI Command Reference

`kestrix` is distributed as a single static binary containing all developer, server, runner, and administrative tools.

## Global Synopsis

```bash
kestrix <subcommand> [flags] [arguments]
```

To see available subcommands and flags:
```bash
kestrix --help
```

To display the built version and commit:
```bash
kestrix version
```

---

## 1. `kestrix serve`

Starts the HTTP webhook server, REST API, and static UI frontend.

```bash
kestrix serve [flags]
```

### Flags & Environment Variables

| Flag | Env Variable | Default | Description |
|------|--------------|---------|-------------|
| `--addr` | `KESTRIX_LISTEN_ADDR` | `:8080` | TCP host and port to listen on. |
| `--config` | `KESTRIX_CONFIG` | `~/.kestrix/config.yaml` | Path to YAML configuration file. |
| `--database-url` | `DATABASE_URL` | `postgres://...` | PostgreSQL connection string (required). |
| `--static` | `KESTRIX_STATIC_DIR` | (none) | Directory containing compiled frontend assets (e.g., `./ui/dist`). |
| `--secret` | `KESTRIX_WEBHOOK_SECRET` | (none) | Fallback shared secret for webhook verification. |
| `--method` | `KESTRIX_WEBHOOK_METHOD` | `gitlab` | Webhook verification method (`gitlab` header token or `hmac` SHA-256). |
| `--dispatch` | `KESTRIX_DISPATCH_PROJECT` | (none) | GitLab project ID to dispatch downstream pipelines to. |
| `--gitlab-token` | `KESTRIX_GITLAB_TOKEN` | (none) | GitLab API access token for downstream dispatch. |
| `--gitlab-url` | `KESTRIX_GITLAB_URL` | `https://gitlab.com` | GitLab instance URL. |
| `--source-cache-dir` | `KESTRIX_SOURCE_CACHE_DIR` | `~/.kestrix/cache` | Directory for bare Git object caches. |
| `--source-cache-max-age` | `KESTRIX_SOURCE_CACHE_MAX_AGE` | `168h` (7d) | Retention time for cached Git repositories. |
| `--source-cache-max-bytes` | `KESTRIX_SOURCE_CACHE_MAX_BYTES` | `10737418240` (10G) | Storage limit for repository cache. |

### Required Environment Variables

| Variable | Description |
|----------|-------------|
| `KESTRIX_MASTER_KEY_PATH` | Path to the 32-byte master key file for AES-256-GCM credential encryption (`0600`). |
| `KESTRIX_JWT_SIGNING_KEY_PATH` | Path to the 32-byte JWT signing key file for user session tokens (`0600`). |

### Examples

```bash
# Start server with default settings
kestrix serve

# Start server serving the production UI and using custom database
kestrix serve --addr :8080 --static ./ui/dist --database-url postgres://user:pass@db:5432/kestrix
```

---

## 2. `kestrix runner`

Runs a long-polling remote runner worker that executes assigned tasks and streams output back to the Kestrix server.

```bash
kestrix runner [flags]
```

### Flags & Environment Variables

| Flag | Env Variable | Default | Description |
|------|--------------|---------|-------------|
| `--server` | `KESTRIX_RUNNER_SERVER` | (required) | Base URL of the Kestrix server (e.g. `https://kestrix.example.com`). |
| `--token` | `KESTRIX_RUNNER_TOKEN` | (required) | Bearer token generated when registering the runner in Kestrix. |
| `--mode` | `KESTRIX_RUNNER_MODE` | `local` | Execution backend: `local` (host bash) or `docker`. |

### Runner Environment Controls

| Variable | Default | Description |
|----------|---------|-------------|
| `KESTRIX_RUNNER_DOCKER_PIDS` | `256` | Maximum PIDs per task container in `docker` mode. |
| `KESTRIX_RUNNER_DOCKER_MEMORY` | `2g` | Maximum memory limit per container. |
| `KESTRIX_RUNNER_DOCKER_CPUS` | `2` | CPU limit per container. |
| `KESTRIX_RUNNER_DOCKER_NETWORK` | `bridge` | Network mode (`bridge` or `none` for network isolation). |

### Example

```bash
kestrix runner \
  --server https://ci.company.internal \
  --token b7f2c14092b3... \
  --mode docker
```

---

## 3. `kestrix validate`

Validates an HCL pipeline definition file for syntax errors, unknown blocks, and dependency cycles.

```bash
kestrix validate <pipeline.tf>
```

### Example

```bash
kestrix validate pipeline.tf
# Output:
# ✓ Pipeline is valid: 3 repos, 5 tasks, no dependency cycles
```

---

## 4. `kestrix resolve`

Dry-runs trigger expressions against a set of changed repository names and outputs which tasks would execute, along with their execution order.

```bash
kestrix resolve [--changed repo1,repo2] <pipeline.tf>
```

### Flags

| Flag | Description |
|------|-------------|
| `--changed` | Comma-separated list of repository names that had modifications. |

### Example

```bash
kestrix resolve --changed auth_service,shared_lib pipeline.tf
# Outputs resolved tasks, stage ordering, and skipped dependencies
```

---

## 5. `kestrix run`

Executes a pipeline locally on the current machine without needing a full server deployment.

```bash
kestrix run [flags] <pipeline.tf>
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--changed` | (all) | Comma-separated list of modified repositories to evaluate triggers for. |
| `--workdir` | `.` | Working directory containing the checkout. |
| `--include-manual` | `false` | When true, executes tasks marked `manual = true` without waiting for approval. |

### Example

```bash
kestrix run --changed backend ci/pipeline.tf
```

---

## 6. `kestrix variables`

Inspects and resolves pipeline variables for a given repository, task, and target environment.

```bash
kestrix variables <pipeline.tf> [--repo <name>] [--job <name>] [--env <environment>]
```

---

## 7. `kestrix user`

Manages local user accounts and roles.

```bash
kestrix user <command> [arguments]
```

### Subcommands

#### `kestrix user create`
Creates a new user. The password is requested interactively via secure terminal prompt:

```bash
kestrix user create --role [viewer|operator|admin] <username>
```

#### `kestrix user list`
Lists all registered users, their roles, and creation timestamps:

```bash
kestrix user list
```

#### `kestrix user passwd`
Resets the password of an existing user account:

```bash
kestrix user passwd <username>
```

#### `kestrix user delete`
Permanently deletes a user account:

```bash
kestrix user delete <username>
```

---

## 8. `kestrix keys`

Manages the encryption keys used by Kestrix for securing credentials at rest.

### `kestrix keys rotate-master`

Performs an atomic, online zero-downtime re-encryption of all stored repository credentials, remote jobs, and run secrets with a newly generated master key.

```bash
kestrix keys rotate-master --new-key-path /path/to/new.key [--database-url postgres://...]
```

#### Rotation Procedure:
1. Generate new 32-byte key: `head -c 32 /dev/urandom > /path/to/new.key && chmod 0600 /path/to/new.key`
2. Run rotation: `kestrix keys rotate-master --new-key-path /path/to/new.key`
3. Replace the active key file at `KESTRIX_MASTER_KEY_PATH` with the new key.
4. Restart the Kestrix server.
