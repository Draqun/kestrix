## Context

Kestrix currently performs project registration as a side effect of server construction. When `cfg.PipelineDir` is set, `NewServerWithDeps` parses that directory and upserts a project using the HCL project name. The development Docker image starts with `--dir ./examples/demo-mono`, so every restart recreates the deleted `demo-mono` row in PostgreSQL.

Project registration is already exposed through the PostgreSQL-backed CRUD API. Startup should initialize persistence and services only; it must not mutate user-managed project configuration.

```text
Server startup
    |
    +--> connect/migrate PostgreSQL
    |
    +--> build services and routes
    |
    `--> serve requests

Project registration (explicit)
    UI/API --> POST/PUT /api/projects --> PostgreSQL
    UI/API --> DELETE /api/projects/{name} --> PostgreSQL
```

## Goals / Non-Goals

**Goals:**

- Make project CRUD the sole mechanism that creates and deletes registered projects.
- Ensure a deleted project remains deleted after a server restart.
- Remove the development Docker dependency on `examples/demo-mono` as a startup seed.
- Preserve the example files for CLI examples and tests where they are still useful.
- Add regression coverage for startup not creating a project.

**Non-Goals:**

- No database reset or deletion of existing project rows during deployment.
- No removal of the HCL example directory or unrelated CLI commands.
- No redesign of project CRUD or pipeline parsing.

## Decisions

### Remove startup upsert

Delete the `cfg.PipelineDir` parsing and `projectStore.Upsert` block from `NewServerWithDeps`. This is the smallest change that restores the persistence contract and avoids hidden writes during construction.

Alternative considered: keep auto-registration behind a new `--seed-demo` flag. Rejected for this change because the legacy behavior is the source of the problem and no replacement seed workflow is required.

### Stop passing the demo directory in development Docker

Change the development container command so it starts the server without `--dir ./examples/demo-mono`. The mounted repository remains available for project paths and task execution, while registered projects are configured explicitly through the API.

Alternative considered: keep the flag but make the startup upsert idempotent. Rejected because idempotent upsert still recreates projects deleted by users.

### Update the project-registration contract and tests

Replace the auto-load requirement with an explicit-registration requirement. Add a server construction test using a temporary PostgreSQL-backed store or the existing dependency-injected test setup to assert that a configured pipeline directory does not create a project. Update any tests that currently expect automatic registration.

### Security considerations

Removing an implicit startup write reduces unexpected mutation of project configuration and avoids registering a pipeline from a server filesystem path without an explicit API action. Existing path validation in project handlers remains the boundary for pipeline file access; this change does not weaken it.

## Risks / Trade-offs

- [Existing users rely on auto-registration] → Document that projects must be created through the API/UI and preserve the example CLI workflow separately.
- [A deployment expects `--dir` to register its project] → The change is intentionally breaking for that behavior; deployment configuration must create the project explicitly before use.
- [Stale `demo-mono` rows remain in PostgreSQL] → Do not delete data automatically; remove the row once through the UI/API after deploying the change.

## Migration Plan

1. Deploy the code and development Docker command changes.
2. Remove any unwanted `demo-mono` row through `DELETE /api/projects/demo-mono` or the UI.
3. Register required projects explicitly with their repository paths and pipeline directories.
4. Restart the server and verify the project list is unchanged.

Rollback is a code rollback only. Existing project rows remain intact; restoring the previous version restores the old auto-registration behavior.

## Open Questions

- Whether a separate, explicit project-seeding command is needed later for packaged deployments is outside this change.
