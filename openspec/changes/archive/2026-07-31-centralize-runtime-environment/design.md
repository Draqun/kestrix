## Context

Kestrix currently has several independent environment readers. The CLI calls `os.Getenv` for secrets and runtime options, webhook helpers scan `METACI_*`, tests read legacy variables directly, and Docker/templates advertise the old prefix. This makes the rename incomplete and means different entry points can apply different precedence or defaults.

The application already has `internal/config`, but its YAML loader does not own process environment state. The requested singleton should provide one immutable snapshot for the lifetime of a process, while preserving testability and explicit CLI/config-file overrides.

## Goals / Non-Goals

**Goals:**

- Define `KESTRIX_*` as the canonical prefix for Kestrix-owned environment variables.
- Initialize one immutable environment snapshot at process startup.
- Route all application environment reads through `internal/config` or a narrow configuration API.
- Remove legacy aliases entirely; only canonical `KESTRIX_*` names are read by the application.
- Keep generic variables (`DATABASE_URL`, `DOCKER_*`) and provider-owned variables under their existing names unless they are explicitly Kestrix-owned.

**Non-Goals:**

- Dynamic environment reload after startup.
- An external secrets manager.
- Storing secrets in YAML, the singleton logs, or run metadata.
- Renaming historical database fields or unrelated test-only infrastructure in one step.

## Decisions

### Immutable singleton snapshot

Add an environment configuration type in `internal/config`, for example `Environment`, with typed fields such as `WebhookSecret`, `GitLabToken`, `DispatchProject`, `DatabaseURL`, and a nested `Docker` value. A private normalized map is retained only for controlled forwarding to child processes. A `sync.Once`-guarded initializer reads `os.Environ()` exactly once. `Current()` returns the immutable process snapshot; application packages do not call `os.Getenv` or string-key accessors for known settings directly.

The executable initializes the singleton before dispatching any subcommand. Tests use a constructor or an explicit test initializer with an isolated environment snapshot rather than mutating the production singleton.

```text
process start
     |
     v
config.InitializeFromEnv(os.Environ())  -- once -->  immutable Environment
     |                                               |
     +--> CLI/config-file precedence                 +--> typed fields used by web, dispatch, runner, variables
     +--> typed application settings                 +--> child-process environment
```

### Naming and precedence

Kestrix-owned variables use `KESTRIX_`, including server, webhook, dispatch, test, and generated dispatch-context variables. Generic integration variables such as `DATABASE_URL` and `DOCKER_*` remain unchanged. The `.env.template` contains canonical names only.

For a migrated variable, resolution is:

1. explicit CLI flag;
2. YAML/config-file value;
3. canonical `KESTRIX_*` environment variable;
4. application default.

Legacy `METACI_*` names are not read, normalized, forwarded, or used for precedence. Deployments using them must migrate to the canonical names before starting the application.

### Forwarding environment variables

The variables store and dispatch code consume the singleton's canonical Kestrix-prefixed map. Provider-specific variables remain untouched.

### Migration and removal

Update templates, Docker examples, CLI help, README/docs, scripts, tests, and application reads in one change. No package should retain a runtime compatibility alias for the old names.

## Risks / Trade-offs

- [Risk] A package still calls `os.Getenv` directly. → Mitigation: repository-wide search and a test/lint guard for application packages.
- [Risk] Existing deployments still provide old names. → Mitigation: document the required migration and fail configuration audits when old names remain in runtime code.
- [Risk] `sync.Once` complicates tests that need different environments. → Mitigation: expose a pure loader taking an environment list and use it for unit tests; reserve the singleton for process wiring.
- [Risk] Renaming forwarded variables changes child task behavior. → Mitigation: normalize both names during migration and cover dispatch/runner integration tests.

## Security considerations

Environment values are read once and held in memory only. Accessors must not log secret values. Redaction applies to warnings and errors, and the singleton must not be serialized or exposed through API responses. Tests must use synthetic secrets and verify that logs contain names, not values.

## Migration Plan

1. Add the pure environment loader and startup singleton.
2. Migrate runtime reads and prefixed variable forwarding to the singleton.
3. Update `.env.template`, Docker files, scripts, documentation, and tests to `KESTRIX_*`.
4. Deploy with canonical variables.

## Open Questions

- Existing deployments must migrate legacy environment names before upgrading.
