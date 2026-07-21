## Context

The project is branded as **Kestrix** (see `AGENTS.md`, UI title, server log line), but the build still produces `bin/metaci`. This is visible in log lines like `Kestrix server listening on :8080` followed by the process name `./bin/metaci`. The Go module path `gitlab.com/lunaris-bot/metaci` is intentionally kept to avoid import churn.

## Goals / Non-Goals

**Goals:**
- Build the CLI as `bin/kestrix`.
- Update all internal references (Makefile, scripts, Docker, docs, examples) to use `kestrix`.
- Ensure logs and help text identify the tool as Kestrix.

**Non-Goals:**
- Renaming the Go module or package imports.
- Changing API routes, DB schema, or configuration files.
- Renaming the GitLab repository or registry paths.

## Decisions

1. **Keep the Go module path unchanged.**
   - Rationale: Renaming `gitlab.com/lunaris-bot/metaci` would touch every file and break in-flight branches. The user-facing binary name is independent of the module path.
2. **Use a single Makefile variable `BINARY := bin/kestrix`.**
   - Rationale: Centralizes the name; all targets reference `$(BINARY)`.
3. **Update Docker `ENTRYPOINT`/`CMD` to `kestrix`.**
   - Rationale: Containers should run the renamed executable; the compose service name can stay `metaci` for now to avoid network churn.
4. **Update CLI usage and server log strings.**
   - Rationale: The user sees these strings; they are the remaining visible `metaci` references.
5. **Update Swagger/OpenAPI spec title to "Kestrix API".**
   - Rationale: The Swagger UI at `/docs/index.html` currently displays "MetaCI API"; the title must match the new product name. The `docs/docs.go` file is generated from `cmd/metaci/main.go` annotations, so the source annotation and the generated file are updated together.

## Risks / Trade-offs

- **[Risk] External scripts rely on `bin/metaci`.** → Mitigation: This is a breaking change documented in the proposal; the dogfood pipeline and examples are updated in the same PR.
- **[Risk] Developers muscle-memory typed `./bin/metaci`.** → Mitigation: Add a short note in `AGENTS.md` and update the Makefile help text.

## Migration Plan

1. Replace `bin/metaci` with `bin/kestrix` in `Makefile`.
2. Update `AGENTS.md` command examples and path references.
3. Update `docker/Dockerfile` and `docker/docker-compose*.yml` commands.
4. Update `ci/pipeline.tf` and `scripts/*.sh` invocations.
5. Update UI dashboard example command strings.
6. Update CLI usage (`cmd/kestrix/main.go`), server log prefix, and Swagger annotations/spec title.
7. Run `make build`, `make test`, `make pipeline`, and `cd ui && npm run build`.

## Architecture Diagram

```
Developer / CI
      │
      ▼
  make build
      │
      ▼
  bin/kestrix   ◄── renamed from bin/metaci
      │
      ├──► serve ──► Kestrix server listening on :8080
      ├──► resolve ──► pipeline.tf
      └──► run ──► local/Docker/GitLab runner
```

## Security Considerations

- No secrets, auth flows, or network interfaces change.
- The rename only affects the executable path; file permissions and container security contexts remain the same.
