## Why

The application still reads runtime settings directly from `METACI_*` variables in the CLI, webhook helpers, Docker templates, and tests. This leaves the Kestrix rename incomplete and spreads environment lookup across the codebase, making precedence, testing, and future configuration changes difficult to reason about.

## What Changes

- Make `KESTRIX_*` the canonical prefix for Kestrix-owned environment variables and update `.env.template`, documentation, Docker examples, CLI help, and runtime code.
- Introduce an immutable process configuration singleton initialized once during application startup from the process environment.
- Make application code obtain environment-backed values through the singleton/configuration API instead of calling `os.Getenv`, `os.LookupEnv`, or prefix scans directly.
- Define explicit precedence between canonical `KESTRIX_*` variables, supported generic variables such as `DATABASE_URL`, config files, and CLI flags.
- Remove runtime compatibility with legacy `METACI_*` variables so the application has one unambiguous environment contract: canonical `KESTRIX_*` names.
- Rename application-owned generated environment variables and templates where they are part of Kestrix runtime configuration, while keeping third-party/provider names unchanged.
- Ensure secrets are loaded once, never logged, and are not copied into mutable global state after initialization.

## Capabilities

### New Capabilities

- `runtime-configuration`: Load and expose immutable process configuration through a startup-initialized singleton with canonical Kestrix environment names.

### Modified Capabilities

None. Existing CLI and service behavior is preserved while the configuration source and naming contract are centralized.

## Impact

- `.env.template`, README/docs, Docker templates, and deployment examples.
- `cmd/kestrix`, `internal/config`, `internal/web`, `internal/variables`, dispatch/webhook code, and tests.
- Startup initialization and command/config precedence.
- Existing deployments using `METACI_*` require migration to `KESTRIX_*` before upgrading.

## Non-goals

- Introducing an external secrets manager or remote configuration service.
- Changing unrelated generic variables such as `DATABASE_URL`, `DOCKER_*`, or provider-defined GitLab variables unless they are explicitly Kestrix-owned.
- Making runtime configuration dynamically reloadable after startup.
- Renaming historical OpenSpec text or immutable database schema fields.
