## Context

The current runtime has one PostgreSQL identity across development and deployment:

- local `serve` fallback: `postgres://kestrix:kestrix@localhost:5432/kestrix`;
- development compose: PostgreSQL exposed on host port `5432`, database `kestrix`, volume `postgres_data`;
- production compose: PostgreSQL exposed on host port `5432`, database `kestrix`, volume `postgres_data`.

The historical separation was different. Before `4ae1b40`/`ae3f140`, the development compose did not inject the production-style `DATABASE_URL`, and the application still had a file-backed local fallback. `4ae1b40` added PostgreSQL configuration to the local Docker stack; `ae3f140` removed `FileStore` and added the hardcoded local PostgreSQL fallback. That combination made local and deployed processes converge on the same database identity.

```text
Local host app  -- localhost:5433/kestrix_local --+--> local PostgreSQL
                                                  |    volume: kestrix_local_postgres_data
Local Docker app -- postgres:5432/kestrix_local --+

Deployed app    -- postgres:5432/kestrix -------> deployment PostgreSQL
                                                     volume: postgres_data
```

## Goals / Non-Goals

**Goals:**

- Make local development and deployment use distinct database names, host ports, and volumes.
- Make the local host fallback match the local compose database.
- Keep deployment configuration explicit and unchanged in its internal Docker network.
- Add tests/checks that prevent accidental reuse of the same runtime database identity.
- Document the old source of the regression and data migration implications.

**Non-Goals:**

- Do not reintroduce `FileStore`.
- Do not change PostgreSQL schemas or API payloads.
- Do not automatically copy, merge, or delete existing data.
- Do not expose PostgreSQL publicly beyond the existing local bind address policy.

## Decisions

### Use a dedicated local profile

Use `kestrix_local` as the local database name, map the local compose PostgreSQL service to host port `5433`, and use a dedicated volume such as `kestrix_local_postgres_data`. The local Kestrix container uses `postgres:5432/kestrix_local` over the compose network; a host-run binary uses `localhost:5433/kestrix_local`.

Alternative: retain port `5432` and only change the database name. Rejected because a local and deployed stack on the same host could still collide on container/volume ownership and port binding.

### Keep production identity separate and explicit

Production continues to use `DATABASE_URL=postgres://...@postgres:5432/kestrix`, its own compose project/volume, and the existing production port. It must not depend on the local fallback.

Alternative: choose the database based on the process environment implicitly. Rejected because explicit compose environment variables make the boundary auditable and testable.

### Make the local fallback match local compose

Change the no-`DATABASE_URL` fallback used by `serve` to the local host URL. Explicit `DATABASE_URL` always wins, so production and custom deployments remain configurable.

### Verification and security

Add a configuration-level test or validation script asserting that local and deployment URLs differ in database name and host port. Keep credentials in environment variables/templates only; do not add secrets to committed compose files. PostgreSQL should continue to bind locally for development rather than to an unrestricted external interface.

## Risks / Trade-offs

- [Existing local data is in `kestrix`] → Document that it is not moved automatically; provide an explicit dump/restore path if needed.
- [Port `5433` is already occupied] → Allow the local host port to remain configurable with a `POSTGRES_PORT`-style variable, defaulting to `5433`.
- [Operators run compose files without a project name] → Use distinct explicit volume/container names or documented compose project names so Docker resources cannot silently overlap.
- [A deployment relies on the old local fallback] → Require production to set `DATABASE_URL` explicitly and fail fast if it is absent or invalid.

## Migration Plan

1. Deploy the separated compose/configuration changes.
2. Start the local stack; it creates `kestrix_local` in the local volume.
3. If local data from the old shared `kestrix` database is needed, export/import it deliberately into `kestrix_local`.
4. Keep production data in the deployment PostgreSQL volume and verify its `DATABASE_URL` remains the production URL.
5. Remove or archive the old shared local data only after confirming it is not needed.

Rollback restores the old URLs and compose resource names, but must be coordinated with any data written to the new local database.

## Open Questions

- Whether the project should add a first-class `make docker-up-local` target or keep `make docker-up` as the local profile entry point.
