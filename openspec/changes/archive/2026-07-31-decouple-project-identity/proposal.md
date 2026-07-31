## Why

The repository still identifies the code as `gitlab.com/lunaris-bot/metaci`, although the product is now Kestrix and its future Git provider, owner, and repository name are undecided. This leaks obsolete hosting metadata into Go imports, build tooling, containers, tests, and documentation, making a provider migration look like a code move and obscuring the stable identity of the project.

## What Changes

- **BREAKING**: Change the Go module identity from the GitLab repository URL to a provider-neutral Kestrix module path and update all internal imports and linker/coverage references.
- Establish one canonical project identity (`Kestrix`) for binaries, package tooling, container defaults, and documentation.
- Move the repository source location to explicit, replaceable metadata so changing Git provider, owner, or repository name does not require changing Go package imports.
- Remove obsolete `lunaris-bot/metaci` links and defaults from active code, scripts, Docker configuration, and user-facing documentation.
- Add verification that no active build/runtime reference depends on the former provider-owned module path.

## Capabilities

### New Capabilities

- `project-identity`: Defines the stable product/module identity and the separately configurable source-location metadata used by tooling and documentation.

### Modified Capabilities

- `project-docs`: Documentation SHALL obtain repository links from the replaceable source-location metadata rather than embedding provider- or owner-specific URLs.

## Impact

- `go.mod`, all Go imports, version linker flags, coverage scripts, tests, Docker image defaults, CI pipeline examples, README, and business-site links.
- Downstream consumers importing the old module path must update imports (**BREAKING**).
- No runtime API or persisted project-data schema change is intended.

## Non-goals

- Choosing or migrating to a specific Git provider, owner, or hosting account.
- Renaming every historical OpenSpec/archive document or local compatibility directory in one pass.
- Changing Kestrix product behavior, API routes, or pipeline semantics.
