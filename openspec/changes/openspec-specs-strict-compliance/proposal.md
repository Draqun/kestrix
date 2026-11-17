# Proposal: openspec-specs-strict-compliance

## Why

Running `openspec validate --all --strict` currently fails on 31 of the 52 main capability specifications under `openspec/specs/*/spec.md`. The failures stem from two issues introduced when earlier changes were archived:

1. Placeholder `## Purpose` sections containing `TBD - created by archiving change ... Update Purpose after archive.` (flagged as too brief/placeholder by strict validation).
2. Overly dense requirement bodies exceeding the 500-character strict limit in capabilities such as `access-control`, `http-server-hardening`, `observability`, `pipeline-definition`, `project-registration`, `project-sources`, `remote-runner-execution`, `remote-source-acquisition`, `repository-credentials`, `run-attribution`, `run-lifecycle`, `run-scheduling`, `runtime-configuration`, `runtime-secrets`, `task-execution-isolation`, `ui-project-webhook-setup`, `user-authentication`, `webhook-log-history`, and `webhook-receiver`.

Because 31 specs fail validation, `openspec validate --all --strict` cannot be used as a CI quality gate.

## What Changes

- Replace every `TBD - created by archiving change...` placeholder under `## Purpose` across all affected `openspec/specs/*/spec.md` files with clear, accurate 1–2 sentence capability purpose descriptions.
- Refactor requirement descriptions exceeding 500 characters in `openspec/specs/*/spec.md` into concise normative `SHALL`/`MUST` statements while preserving all technical details in their `#### Scenario:` blocks.
- Verify `openspec validate --all --strict` passes 52/52 specifications with zero errors and zero warnings.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `project-docs`: requires all canonical specifications in `openspec/specs/` to pass `openspec validate --all --strict` without placeholder purpose sections or oversized requirement blocks.

## Non-goals

- Modifying runtime Go or React code.
- Rewriting historical archived changes inside `openspec/changes/archive/`.
- Adding or removing functional product requirements.

## Impact

- Edits 31 specification files under `openspec/specs/*/spec.md`.
