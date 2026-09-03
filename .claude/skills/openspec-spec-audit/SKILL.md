---
name: openspec-spec-audit
description: Produce an evidence-based checkbox audit for one named OpenSpec specification. Use when the user names a spec and asks what is implemented, what remains, requirement details, an implementation checklist, or verification evidence.
allowed-tools: Bash(openspec:*), Bash(python3:*), Bash(rg:*), Bash(make:*), Bash(go:*), Read, Grep, Glob
license: MIT
compatibility: Requires openspec CLI, python3, and ripgrep.
metadata:
  author: kestrix
  version: "1.0"
---

# OpenSpec Spec Audit

Audit one main spec from `openspec/specs/<name>/spec.md`. Do not use an archived change's delta spec when the user gives a main spec name.

## Workflow

1. Accept the spec name or resolve it from `openspec list --specs`. If no unique name is available, ask for the name.
2. Run the inventory to enumerate the exact requirement headings:
   ```bash
   python3 .claude/skills/openspec-spec-audit/scripts/spec_inventory.py --spec <name>
   ```
3. Read the full main spec. For every requirement and scenario, inspect production code and tests using `rg`; run the narrowest relevant verification through a Makefile target when available.
4. Produce one checklist item per requirement, including a concise verdict and evidence:
   - `[x]` implemented — identify the supporting implementation and test/verification.
   - `[~]` partial — say precisely which required behavior or scenario is missing.
   - `[ ]` not implemented — say what is absent.
   - `[?]` unknown — state the missing evidence and the next check.
5. Finish with totals and a prioritized remaining-work checklist. Keep all items unchecked except fully implemented requirements.

## Accuracy rules

- Do not treat `(implemented)` or `(pending)` in the Markdown as proof. Mention it only as "declared status" when it disagrees with the audit.
- An implementation without a test is not automatically incomplete, but distinguish tested evidence from code inspection.
- Every SHALL and every scenario is in scope. If a requirement has multiple scenarios, assess them together and mark it partial if any mandatory behavior is missing.
- Do not modify code, specs, or task files during the audit unless explicitly asked.

## Kestrix repo notes

- **Makefile is the source of truth** — verify with `make test` or `make quality`, never by invoking tools ad hoc. Narrow runs: `go test ./internal/dag/ -v -count=1`, `go test -run TestWebhook ./internal/web/ -count=1`.
- **Store and run tests need PostgreSQL** via `KESTRIX_TEST_DATABASE_URL` (a DB name ending in `_test`; `make docker-up` creates `kestrix_test`). If the DB is unavailable, classify affected requirements `[?]` with the blocked command as the missing evidence — never `[ ]`.
- **Where to look:** backend under `internal/` and `cmd/kestrix/`; HTTP handlers and generator in `internal/web/`; DAG and trigger expressions in `internal/dag/`; HCL parsing in `internal/parser/`; persistence in `internal/store/`. Frontend requirements live in `ui/src/`, verified with `cd ui && npm run build` plus code inspection.
- **Documentation requirements** are real requirements: check `docs/`, `README.md`, and Swagger annotations before marking them implemented.

## Output shape

```text
Spec: dag-engine — 4/5 implemented · 1 partial

- [x] Boolean Expression Evaluation — `internal/dag/expr.go`; verified by `make test`.
- [~] Documentation inclusion — engine docs exist, but no overview link was found.

Remaining
- [ ] Add the missing overview link for DAG Engine documentation.
```
