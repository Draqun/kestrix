## Context

The server currently constructs PostgreSQL-backed stores when possible and falls back to a JSON FileStore when the database cannot be reached. Project reads can continue in that mode, but run persistence cannot: `RunService` receives a nil run store and can return a zero identifier while execution continues in the background. This creates an apparently successful run that cannot be queried, canceled, or displayed.

The application is now database-first: Docker, CI, configuration, and run APIs assume PostgreSQL. The implementation should make that assumption explicit and fail during startup, before accepting HTTP requests.

```text
configuration
     |
     v
connect + migrate PostgreSQL ---- failure ---> startup error, no HTTP server
     |
     +--> PG project store
     +--> PG run store
     +--> HTTP API and RunService
```

## Goals / Non-Goals

**Goals:**

- Make PostgreSQL the only persistence backend.
- Remove FileStore code, fallback wiring, and obsolete JSON-store tests/docs.
- Ensure startup fails clearly when the database is unavailable or its schema is invalid.
- Guarantee every accepted run has a persisted positive `run_id`.
- Provide an explicit migration path for existing JSON project data.

**Non-Goals:**

- Changing PostgreSQL schema semantics beyond what is required for startup validation.
- Supporting a second database backend.
- Automatically deleting old JSON files or database resources.

## Decisions

### PostgreSQL is validated before serving

`NewServer`/the CLI startup path will establish PostgreSQL stores and return an error instead of silently constructing a FileStore. The database connection and required schema must be checked before `ListenAndServe`. The HTTP API must never run in a degraded persistence mode.

Alternative considered: retain FileStore for project CRUD. Rejected because it makes the persistence contract differ between project and run APIs and is the direct cause of zero-valued run identifiers.

### Remove FileStore rather than leave an unused compatibility package

Delete the JSON store implementation and its production references. Tests that need persistence use a PostgreSQL test database or injected mocks. Existing JSON data is handled by a documented one-time migration, not by keeping a second runtime backend.

### Run creation remains transactional at the store boundary

`RunService.Start` continues to create the run before launching the background goroutine. A failed insert returns an HTTP error; a successful insert returns its database-generated positive ID. A nil run store becomes an invalid construction/startup state and is no longer treated as a valid no-persistence mode.

### Security considerations

Database URLs and passwords remain environment/configuration values and must not be logged. Startup errors may identify that database initialization failed, but must redact credentials and query strings containing secrets.

## Risks / Trade-offs

- [Risk] Existing installations rely on JSON project files. → Mitigation: document and test a one-time import/migration path before upgrade.
- [Risk] Local development without PostgreSQL no longer starts. → Mitigation: keep `docker-up` as the supported local prerequisite and make the startup error actionable.
- [Risk] A schema mismatch prevents startup. → Mitigation: validate schema explicitly and document the required initialization command.

## Migration Plan

1. Start PostgreSQL and ensure the Kestrix role, databases, and schema exist.
2. Import any existing FileStore project records into PostgreSQL using a one-time migration procedure/tool.
3. Deploy the version that removes FileStore.
4. Verify project listing and a pipeline run return a persisted positive run ID.
5. Roll back the binary if needed; retain the old JSON file as a recovery source, but do not use it as an active runtime backend.

## Open Questions

- Should the JSON-to-PostgreSQL import be implemented as a one-off CLI command or documented SQL/manual procedure?
