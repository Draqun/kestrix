# Tasks: openspec-specs-strict-compliance

## 1. Replace Placeholder Purpose Sections

- [ ] 1.1 Replace `TBD - created by archiving change...` in `## Purpose` across all affected `openspec/specs/[a-m]*/spec.md` files with descriptive capability summaries.
- [ ] 1.2 Replace `TBD - created by archiving change...` in `## Purpose` across all affected `openspec/specs/[n-z]*/spec.md` files with descriptive capability summaries.

## 2. Condense Oversized Requirements & Verify Strict Compliance

- [ ] 2.1 Refactor >500-character requirement paragraphs in `access-control`, `http-server-hardening`, `observability`, `pipeline-definition`, `project-registration`, `project-sources`, `remote-runner-execution`, `remote-source-acquisition`, `repository-credentials`, `run-attribution`, `run-lifecycle`, `run-scheduling`, `runtime-configuration`, `runtime-secrets`, `task-execution-isolation`, `ui-project-webhook-setup`, `user-authentication`, `webhook-log-history`, and `webhook-receiver` specs so each requirement is ≤500 characters while preserving all normative rules and scenarios.
- [ ] 2.2 Run `openspec validate --all --strict` and verify all 52 specs pass with zero errors and warnings.
