## Why

We need comprehensive documentation for the Kestrix project to give stakeholders and developers a clear overview of what the project consists of and how its parts interact. This documentation will serve as a reference for the pipeline, onboarding, and future enhancements.

## What Changes

- Add a high‑level project documentation artifact.
- Create dedicated sections for each major component:
  - Front‑end (FE)
  - Back‑end (BE)
  - CI tools
  - Terraform provider
  - Business pages (content/public‑facing documentation)
- Produce a design document that elaborates on the structure and dependencies of these sections.
- Generate a task list breaking the work into manageable steps.

## Capabilities

### New Capabilities
- `project-docs`: Documentation covering the overall architecture and each component of the Kestrix project.

### Modified Capabilities
- *(none)*

## Impact

- Improves onboarding for new contributors and stakeholders.
- Provides a single source of truth for the pipeline composition.
- No code changes; only documentation files are added.

## Non‑goals

- This change does **not** modify any existing source code or behaviour.
- It does not implement new features in FE, BE, CI tools, or the Terraform provider; it only documents them.
