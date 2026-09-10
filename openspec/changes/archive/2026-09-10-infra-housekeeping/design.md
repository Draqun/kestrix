## Context

See proposal.md - Why. What shapes the approach:

- `docker/docker-compose.yml:80-99` and `docker/docker-compose.prod.yml:80-90`
  declare a `redis` service and `redis_data` volume. No service lists it in
  `depends_on`, so removing it is a deletion with no dependency edge to repair.
- `go.mod` has no Redis module. `grep -ri redis` over `cmd/` and `internal/`
  returns nothing. The application cannot notice the service leaving.
- `scripts/docker-deployment-e2e.sh:11,99` derives and exports `REDIS_PORT` so
  Compose interpolation has a value. Once the service is gone the variable is
  unused, and Compose warns on nothing when an exported variable has no
  consumer, so it can simply go.
- `openspec/specs/docker-project-identity/spec.md` names Redis in two scenarios
  of the identity requirement. That spec, not the application, is the one
  contract that currently requires Redis.
- `docs/terraform-provider.md` documents a `kestrix_project` resource schema
  for code in `https://gitlab.com/kestrix/terraform-provider-kestrix`. Two
  copies of a schema drift; the provider repository is the one that compiles.

## Goals / Non-Goals

**Goals:**

- Compose declares exactly what Kestrix connects to: the application,
  PostgreSQL, and (for now) the business site.
- One place documents the Terraform provider's resources: its own repository.
- No change to any environment variable Kestrix reads, any port Kestrix
  publishes, or any database name.

**Non-Goals:**

- Touching the business site in any file. It stays, per the proposal.
- Rewriting historical release notes. `CHANGELOG.md` `v0.2.0` entries stay as
  written; Unreleased gets the removal.

## Architecture

Before and after, production Compose (dev is the same minus the business
site's separate port numbering):

```
  before                                   after

  ┌──────────┐  :18080  ┌──────────┐       ┌──────────┐  :18080  ┌──────────┐
  │ operator │─────────▶│ kestrix  │       │ operator │─────────▶│ kestrix  │
  └──────────┘          └────┬─────┘       └──────────┘          └────┬─────┘
       │  :8082              │ DATABASE_URL     │  :8082              │ DATABASE_URL
       ▼                     ▼                  ▼                     ▼
  ┌──────────┐          ┌──────────┐       ┌──────────┐          ┌──────────┐
  │ business │          │ postgres │       │ business │          │ postgres │
  └──────────┘          └──────────┘       └──────────┘          └──────────┘
                        ┌──────────┐
       :6379 ──────────▶│  redis   │  ◀── nothing connects
                        └──────────┘
```

## Decisions

### D1. Delete Redis rather than keep it "for later"

The dev Compose comment says "for future rate limiting / caching". The rate
limiter arrived in `rbac-hardening` and is deliberately in-process; the cache
arrived as `IdentityCache`, also in-process. The future the comment waited for
came and did not need Redis.

Alternative considered: keep the service under a Compose profile so it is
opt-in. Rejected: a profile still ships an image reference and a volume
definition to maintain, and there is no consumer to opt in for. If replicas
ever need a shared throttle store, that change adds the dependency together
with the code that uses it.

### D2. Leave `redis_data` volumes on existing hosts

`docker-project-identity` already requires that Compose changes never delete
existing containers, volumes, or data implicitly. Removing the volume
definition from the file means `docker compose up` stops managing it and
`docker compose down -v` no longer sees it; the data stays until an operator
runs `docker volume rm`. The CHANGELOG entry names the volume and the command.

### D3. The provider doc becomes a pointer, not a mirror

The doc keeps three facts that are true from this repository's point of view:
what the provider manages (project registration), where it lives, and how it
authenticates (the same bearer token as `docs/authentication.md`, so the same
`viewer`/`operator`/`admin` rules apply to a Terraform run). Everything that
belongs to the provider's release cycle - resource arguments, example HCL,
status - is a link.

Alternative considered: delete the doc and the `docs/overview.md` row.
Rejected: `project-docs` REQ-2 lists the provider as a documented component,
and a reader of this repository still needs the pointer.

### D4. Vendored skill text is out of scope

`.claude/`, `.opencode/`, and `.agents/` contain an OpenSpec skill whose
example prose says "Your design mentions using Redis". That is tooling text
shipped by the skill, not a statement about this project. Editing it would be
overwritten on the next skill update.

## Security considerations

The production Compose file publishes Redis on `${REDIS_PORT:-6379}` on every
host interface, with no password, because `redis:7-alpine` runs without one by
default. Nothing reads it, so the exposure buys nothing. Removing the service
removes an unauthenticated listener from every production deployment. No
credential, key path, or `KESTRIX_*` variable changes.

## Risks / Trade-offs

- [An operator has scripts that run `docker compose exec redis ...`] → Those
  fail with "no such service" immediately and loudly. The CHANGELOG entry
  names the removal.
- [`scripts/docker-deployment-e2e.sh` breaks if a Compose variable is left
  without a value] → Removing `REDIS_PORT` from the script together with the
  service that interpolated it keeps `docker compose config` clean. Task 4.2
  verifies with `config` before the full E2E run.
- [The `docker-project-identity` main spec keeps naming Redis if the delta is
  written partially] → The delta copies the full requirement block as the
  archive step requires.

## Migration Plan

1. Merge; pull on the deployed host.
2. `docker compose -f docker/docker-compose.prod.yml up -d --remove-orphans`
   stops and removes the running `redis` container.
3. Optionally `docker volume rm <project>_redis_data` once satisfied nothing
   needs it. Nothing in Kestrix ever wrote to it.
4. Rollback: check out the previous revision and `up -d`; the volume, if not
   removed, is reattached unchanged.
