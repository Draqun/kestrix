## Context

Kestrix has a production Compose definition and a deployment script that builds or selects an image, recreates the stack, and waits for `/health`. Existing tests validate components, but they do not start the real deployment stack and verify it through its published HTTP interface. The test must coexist with local development services, preserve developer data, and leave useful diagnostics when startup fails.

## Goals / Non-Goals

**Goals:**

- Verify the same Docker image and Compose deployment path used by local deployment.
- Keep test resources isolated with a dedicated Compose project, ports, and disposable data volume.
- Test readiness, API reachability, database-backed project/run access, and cleanup.
- Make the test usable from a single Makefile target and from CI.

**Non-Goals:**

- Testing remote SSH/registry deployment.
- Validating every pipeline task or frontend browser interaction.
- Reusing the developer or production database.

## Decisions

### Dedicated E2E Compose project

The test SHALL invoke Docker Compose with a unique project name and explicit host ports. It will use a temporary override or generated environment values rather than modifying the checked-in production Compose file. This prevents collisions with `kestrix-dev-*` and `kestrix-*` services.

```text
make docker-e2e
        |
        v
 build image -> compose up (isolated project/ports/volume)
        |                         |
        |                         v
        |                 wait /api/health
        |                         |
        +-----------------> API smoke checks
                                  |
                                  v
                       collect logs -> compose down -v
```

### Black-box shell test

The test will be a Bash script using `set -euo pipefail`, `curl`, and Docker Compose. Shell-level orchestration matches the deployment mechanism, avoids adding a second test framework, and makes it straightforward to run in GitLab CI. HTTP assertions will check status codes and expected JSON fields; failures will print Compose service status and logs before cleanup.

### Disposable database

The E2E stack will use a unique temporary PostgreSQL volume and deployment database URL. It MUST never point at `kestrix_local`, `kestrix_test`, or the persistent production volume. Cleanup will use the same Compose project and `down --volumes --remove-orphans`.

### Image strategy

The test will build the deployment image locally with a deterministic test tag, then pass that tag to the production Compose invocation. This exercises the Dockerfile and runtime image without requiring registry credentials. An environment override may allow CI to supply a prebuilt image.

### Scope of smoke checks

After readiness, the test will verify `/api/health`, list projects, and request a known API resource or assert the expected empty-state response. It will not require a preexisting project or run, because the disposable database starts empty. A separate seed step may register a minimal fixture project if the API contract requires one.

## Risks / Trade-offs

- **[Docker daemon unavailable]** → Fail fast with a clear prerequisite message.
- **[Slow image build/startup]** → Use bounded readiness retries and reuse Docker build cache where available.
- **[Port collision]** → Use configurable high test ports and fail before startup if they are occupied.
- **[Sensitive logs]** → Do not print environment secrets; redact deployment variables and use only local test credentials.
- **[Stale resources after interruption]** → Install a trap that collects logs and removes only the dedicated Compose project and volume.

## Security Considerations

The E2E stack uses local-only bind addresses, test credentials, and disposable storage. The script MUST NOT use production credentials, push images, or connect to a remote host. Any credentials passed to Compose will come from test-specific environment variables and will not be committed.
