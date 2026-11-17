# Design: openspec-specs-strict-compliance

## Context

When `openspec archive` creates a brand-new capability specification in `openspec/specs/<capability>/spec.md`, it scaffolds `## Purpose` with `TBD - created by archiving change <name>. Update Purpose after archive.` Additionally, OpenSpec strict validation enforces that each `### Requirement:` paragraph stays concise (under 500 characters) and delegates detailed input/output specifics to `#### Scenario:` blocks. Currently 31 of 52 specs in `openspec/specs/` fail strict validation.

```
+-----------------------------------+
| openspec/specs/*/spec.md (52)     |
| - 31 failing strict validation    |
|   * TBD Purpose placeholders      |
|   * >500 char Requirement blocks  |
+-----------------+-----------------+
                  |
                  v
+-----------------------------------+
| Editorial Remediation             |
| - Replace TBD Purpose sections    |
| - Condense Requirement paragraphs |
| - Preserve all WHEN/THEN details  |
+-----------------+-----------------+
                  |
                  v
+-----------------------------------+
| openspec validate --all --strict  |
| -> 52 passed, 0 failed            |
+-----------------------------------+
```

## Goals / Non-Goals

**Goals:**
- Achieve 100% pass rate (`52 passed`) on `openspec validate --all --strict`.
- Preserve every normative behavior and scenario across all 52 capability specs.

**Non-Goals:**
- Editing archived files under `openspec/changes/archive/`.
- Changing application code or tests.

## Security Considerations

- Security-critical specifications (`access-control`, `http-server-hardening`, `repository-credentials`, `runtime-secrets`, `task-execution-isolation`, `user-authentication`, `webhook-receiver`) must retain every normative security invariant (`SHALL`/`MUST`) when condensing >500-character requirement text into requirement + scenario pairs.

## Decisions

### D1: Move enumerations and edge-case details from Requirement prose into Scenario bullets
- **Choice:** Keep the `### Requirement:` statement focused on the core normative rule (<500 chars) and express detailed field lists, status codes, or env var names in `#### Scenario:` (`- **WHEN**` / `- **THEN**` / `- **AND**`) steps.
- **Alternatives considered:** Splitting one requirement into many micro-requirements. Rejected where a single logical invariant with multiple scenarios is clearer and matches existing spec IDs.

## Risks / Trade-offs

- **[Risk]** Inadvertently weakening a normative requirement while shortening prose.
  → *Mitigation:* Keep all `SHALL`/`MUST` obligations intact and verify with `openspec validate --all --strict` and `git diff` / `jj diff` review.
