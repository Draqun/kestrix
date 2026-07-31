## Why

The server currently auto-registers a project from its configured `--dir` on every startup. In the development Docker setup this directory is `examples/demo-mono`, so deleting `demo-mono` through the API or UI is not persistent: the startup bootstrap recreates it and pollutes the project list. The old demo bootstrap should be removed now that projects are managed through the project registration API and PostgreSQL.

## What Changes

- Remove automatic project creation from `cfg.PipelineDir` during server construction.
- Stop using `examples/demo-mono` as a development server pipeline directory or startup seed.
- Preserve project CRUD persistence: a project deleted through the API remains deleted after restart.
- Update tests and documentation that describe automatic demo project registration.
- **BREAKING**: starting the server with `--dir` will no longer create a registered project; `--dir` may remain available only for legacy pipeline-related behavior where still required.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `project-registration`: remove the startup auto-load requirement and require project registration to be explicit and persistent.

## Impact

- Affected backend startup wiring in `internal/web/server.go`.
- Affected development Docker command/configuration and related tests/docs.
- Existing PostgreSQL project data is not migrated or deleted; only the implicit `demo-mono` re-creation behavior is removed.

## Non-goals

- Do not delete the `examples/demo-mono` files if they are still used by CLI examples or tests.
- Do not remove the project CRUD API or change PostgreSQL persistence.
- Do not add a replacement automatic seed project.
