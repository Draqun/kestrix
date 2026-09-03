---
name: openspec-implementation-status
description: Audit and summarize implementation progress across every OpenSpec specification in a repository. Use when the user asks for OpenSpec implementation statistics, what is complete or remaining, a spec-progress dashboard, or an implementation audit across all specs.
allowed-tools: Bash(openspec:*), Bash(python3:*), Bash(rg:*), Bash(make:*), Bash(go:*), Read, Grep, Glob
license: MIT
compatibility: Requires openspec CLI, python3, and ripgrep.
metadata:
  author: kestrix
  version: "1.0"
---

# OpenSpec Implementation Status

Report a repository-wide, evidence-based view of OpenSpec implementation.

## Workflow

1. Run `openspec list --specs` from the repository root. Use it as the authoritative list of current specs.
2. Inventory requirement headings and any status declared in the spec text:
   ```bash
   python3 .claude/skills/openspec-implementation-status/scripts/spec_inventory.py --all
   ```
3. Audit every requirement. Read its scenarios, locate the relevant production code and tests with `rg`, and run the narrowest relevant Makefile-backed verification command. Do not infer completion merely from an `(implemented)` label or an archived change.
4. Classify each requirement:
   - `[x]` implemented: code and a credible verification path support all stated behavior.
   - `[~]` partial: some behavior exists but a stated part is absent or unverified.
   - `[ ]` not implemented: behavior is absent.
   - `[?]` unknown: evidence is insufficient; say what must be checked.
5. Present a compact totals line, one row per spec, and a short "next work" list containing every `[~]`, `[ ]`, and `[?]` requirement. Include file paths and test commands as evidence for non-obvious classifications.

## Accuracy rules

- Keep "declared in spec" separate from the audited result; spec annotations are useful context, not proof.
- Count a requirement once, even if it contains several scenarios. A missing required scenario makes it partial or not implemented.
- Treat documentation, generated assets, and integration behavior as implementation when the requirement requires them.
- Do not edit specs, task checklists, or code unless the user explicitly asks for changes.
- If a full audit would be disproportionate, report the inventory first and label the audit coverage clearly rather than fabricating totals.

## Kestrix repo notes

- **Scale:** this repo currently carries ~30 specs and ~120 requirements, so a full evidence-based audit is expensive. Default to running the inventory first, then auditing in slices — offer the user a per-spec breakdown and let them pick, or state clearly which specs were audited versus only inventoried.
- **Makefile is the source of truth** — `make quality` (fmt-check + vet + test + coverage) is the broad gate; `make test` is the narrower one. Never invoke linters or test tools manually.
- **Store and run tests need PostgreSQL** via `KESTRIX_TEST_DATABASE_URL` (a DB name ending in `_test`; `make docker-up` creates `kestrix_test`). When the DB is unavailable, classify affected requirements `[?]` and name the blocked command — do not count them as not implemented.
- **Where to look:** backend under `internal/` and `cmd/kestrix/`; frontend requirements under `ui/src/`, verified with `cd ui && npm run build` plus code inspection.
- **Per-spec deep dives** belong to the `openspec-spec-audit` skill — point the user there when they want one spec covered in full.

## Output shape

```text
OpenSpec implementation: 42/58 implemented · 6 partial · 7 not implemented · 3 unknown

| Spec | Done | Partial | Remaining | Unknown |
| ...  | ...  | ...     | ...       | ...     |

Next work
- [ ] ci-generator — Documentation inclusion (no generated docs page found)
```
