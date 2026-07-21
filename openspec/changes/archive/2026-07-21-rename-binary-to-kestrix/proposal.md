## Why

The project has been rebranded as **Kestrix**, but the CLI binary is still built as `bin/metaci` and process logs show `./bin/metaci`. This mismatch confuses operators and makes the tool identity inconsistent with the project name, documentation, and UI.

## What Changes

- **BREAKING** Rename the compiled binary from `bin/metaci` to `bin/kestrix`.
- Update all Makefile targets and variables that reference the old binary name.
- Update `AGENTS.md` build instructions and paths to use `bin/kestrix`.
- Update Docker and compose configurations to run `kestrix` instead of `metaci`.
- Update scripts, CI pipeline, and example commands that invoke `./bin/metaci`.
- Update the server startup log and CLI usage/help to identify as **Kestrix**.
- Update the Swagger UI page title and OpenAPI spec title from "MetaCI API" to "Kestrix API".
- Keep the Go module path unchanged (`gitlab.com/lunaris-bot/metaci`) to avoid import-breaking churn; only the user-facing binary name changes.

## Capabilities

### New Capabilities
- `binary-identity`: Defines the user-facing binary name, default paths, and process identity for the Kestrix CLI.

### Modified Capabilities
- (none — this is a build/deployment rename with no API or pipeline behaviour changes)

## Impact

- Local developer workflow: `make build` produces `bin/kestrix`; dogfood scripts call `bin/kestrix`.
- Docker images and compose files use the new executable name.
- Documentation (`AGENTS.md`, README, UI examples) is updated.
- Any external automation relying on `bin/metaci` must switch to `bin/kestrix`.

## Non-goals

- Renaming the Go module or package import paths.
- Renaming the GitLab repository or Docker registry paths.
- Changing the database schema, API routes, or project configuration format.
