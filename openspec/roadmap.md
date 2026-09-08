# Roadmap: security and cross-repo batch (September 2026)

Seven changes proposed together after a documentation and code review. They
are independent OpenSpec changes so each can be reviewed, implemented, and
archived on its own, but they touch shared code and two pairs have an ordering
constraint. Implement in the order below.

| # | Change | Kind | Depends on |
|---|--------|------|------------|
| 1 | `runner-execution-isolation` | security | - |
| 2 | `webhook-ingress-hardening` | security | - |
| 3 | `infra-housekeeping` | chore | - |
| 4 | `run-actor-attribution` | audit | - |
| 5 | `runtime-secrets` | feature | 1 (secrets pass the task env allowlist) |
| 6 | `multi-source-projects` | feature | 2 (per-project webhook verification runs before fan-out) |
| 7 | `session-cookie-auth` | security | - |

## Why this order

- **1 and 2 first.** They close the two findings with the widest blast radius:
  task scripts inheriting the server's environment, and webhook verification
  that is skipped when no secret is configured. Everything after them widens
  what a task or a webhook can reach, so the boundaries go in before the
  features.
- **5 after 1.** Runtime secrets are injected into task environments. The
  allowlist in change 1 defines what an environment may contain; change 5 adds
  pipeline secrets to that set rather than reopening it.
- **6 after 2.** Multi-source routing fans a delivery out to every project that
  declares the pushed repository. Change 2 makes verification per project and
  fail-closed; change 6 assumes that and verifies against each matched
  project's secret.
- **3, 4, 7 anywhere.** They do not share code paths with the others beyond
  the usual store and middleware files.

## Shared conventions the changes rely on

- Schema changes are idempotent `CREATE TABLE IF NOT EXISTS` /
  `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` statements in
  `internal/store/pgstore.go`; there is no migration tool.
- New settings use the `KESTRIX_*` prefix, are modelled in
  `internal/config/environment.go`, and appear in `.env.template` and both
  Compose files.
- `scripts/test.sh` holds `internal/web` and `internal/service` at 100%
  coverage; every new branch needs a test.
- The Terraform provider lives in a separate repository
  (<https://gitlab.com/kestrix/terraform-provider-kestrix>) and talks to the
  API with a bearer token; changes to the API contract must stay compatible
  with it.

## Deliberately left out

- The business website (`ui/business`, `docs/business-website`, the
  `business` Compose service) stays in this repository for now and moves to
  its own repository later.
- Per-project read isolation, external identity providers, MFA, a durable
  audit-log table, external secret managers, and distributed rate limiting
  remain follow-ups, as recorded in each change's Non-goals.
