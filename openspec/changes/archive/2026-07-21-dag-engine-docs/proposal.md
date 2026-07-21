## Why

The DAG engine implementation is complete, but the main spec still lists REQ-1 (documentation inclusion) as pending. Users and contributors need a dedicated documentation page explaining how trigger expressions, dependency ordering, cycle detection, and manual tasks work. Closing this gap finalises the DAG engine capability.

## What Changes

- Create a dedicated documentation page `docs/dag-engine.md` covering the DAG engine concepts and behaviour.
- Ensure the project-docs site build includes the new page and outputs it to `ui/dist/docs`.
- Update the existing `dag-engine` spec to mark REQ-1 as satisfied.

## Capabilities

### New Capabilities

- *(none)*

### Modified Capabilities

- `dag-engine`: REQ-1 (Documentation inclusion) changes from pending to satisfied once the docs page is built into `ui/dist/docs`.

## Impact

- Adds `docs/dag-engine.md`.
- May update the docs build script (`scripts/build-docs.sh`) or project-docs configuration.
- No code or API changes.

## Non-goals

- Rewriting the entire project-docs site.
- Adding new DAG engine features.
- Backend or UI code changes beyond documentation generation.
