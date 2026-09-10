## Why

Two pieces of the repository describe infrastructure that does not exist here:
a Redis service that no Go code connects to, and a Terraform provider document
that describes code living in another repository. Both mislead an operator
reading the Compose files or `docs/`, and the Redis service adds an
unauthenticated listener to the production stack for nothing.

## What Changes

- Remove Redis from both Compose profiles, the Docker Makefile, the top-level
  `docker-up` help text, `.env.template`, the Docker E2E script, and the docs
  that list it as a dependency. `go.mod` has no Redis client and no Go file
  mentions it; the dev Compose comment already calls it "for future rate
  limiting / caching" that never arrived.
- Replace `docs/terraform-provider.md` with a short pointer to the provider's
  own repository, `https://gitlab.com/kestrix/terraform-provider-kestrix`,
  stating what it manages and that it authenticates with the same bearer token
  as the API. The resource schema and release status live in that repository,
  not here.
- Record the Redis removal in `CHANGELOG.md` under Unreleased. Release notes for
  `v0.2.0` keep their historical Redis port entries unchanged.

## Non-goals

- The business website stays where it is: `ui/business`, `docs/business-website`,
  the `business` Compose service, `BUSINESS_PORT`, and `BUSINESS_BIND_HOST` are
  not touched. It moves to its own repository in a later change.
- Adding a rate limiter or cache that would actually use Redis. The login
  throttle is in-process by decision (`rbac-hardening` D-series); a shared
  store is a separate change if replicas ever need one.
- Deleting existing Redis volumes on deployed hosts. Per
  `docker-project-identity`, Compose changes never remove data implicitly;
  the operator removes the orphaned `redis_data` volume explicitly.
- Editing the OpenSpec skill files under `.claude/`, `.opencode/`, and
  `.agents/` that mention Redis as a hypothetical example. They are vendored
  tooling text, not project configuration.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `docker-project-identity`: the "Docker resources use the Kestrix identity"
  requirement names Redis as a dependency that must stay reachable in both
  Compose scenarios. The delta drops Redis from those scenarios so the spec
  stops requiring a service the application never uses.

`project-docs` REQ-2 lists the Terraform provider among documented components.
The document remains, as a pointer, so that requirement is unchanged.

## Impact

- `docker/docker-compose.yml`, `docker/docker-compose.prod.yml`: `redis`
  service and `redis_data` volume removed. Nothing declares `depends_on: redis`,
  so no service reference breaks.
- `docker/Makefile`: `deps-up`, `deps-up-d`, the `up-d` banner, and the
  `status` target lose their Redis lines. Top-level `Makefile` `docker-up`
  comment drops "and Redis".
- `.env.template`: `REDIS_PORT` and its comment removed.
  `scripts/docker-deployment-e2e.sh`: `REDIS_PORT` derivation and export
  removed.
- `AGENTS.md`, `README.md`, `docs/usage.md`, `docker/README.md`,
  `docs/terraform-provider.md`, `docs/overview.md`, `CHANGELOG.md`.
- Existing deployments keep an orphaned `redis_data` volume until an operator
  removes it. No Kestrix behaviour changes; no environment variable the
  application reads is affected.
