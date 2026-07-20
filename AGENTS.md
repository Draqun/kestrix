# AGENTS.md — MetaCI

## What this is
Repo-agnostic CI/CD control plane. HCL pipelines → DAG trigger resolution → local/Docker/GitLab-runner execution. Dogfoods itself via `ci/pipeline.tf` + `scripts/`.

## Commands (Makefile is source of truth)
```bash
make build          # → bin/metaci
make test           # go test ./... -v
make quality        # fmt-check + vet + test + coverage
make dev            # air hot-reload backend :8080
make dev-all        # backend :8080 + vite UI :3000
make ui-build       # production FE → ui/dist
make docker-up      # postgres + redis (docker/)
make pipeline       # run dogfood pipeline locally
```
Single package/test:
```bash
go test ./internal/web/ -v -count=1
go test -run TestWebhook ./internal/web/ -count=1
```

## Layout that matters
| Path | Role |
|------|------|
| `cmd/metaci/` | CLI: serve, run, resolve, validate |
| `internal/web/server.go` | All HTTP API + async run handler |
| `internal/runner/` | Run / RunLocal / RunGitLabRunner |
| `internal/dag/` | Trigger expr + task ordering |
| `internal/parser/` | HCL → model.Project |
| `internal/store/pgstore.go` | PG projects + runs (not file store in prod) |
| `ci/pipeline.tf` | Dogfood pipeline |
| `scripts/*.sh` | Job scripts (called via `bash scripts/...`) |
| `ui/` | React + MUI + React Flow |

## Runtime facts agents miss
- **Default runner mode is `local`** (host bash), not Docker. Config: `Runner.Mode` / env.
- **DB is PostgreSQL** via `DATABASE_URL` / `--database-url`. Runs/history need PG; file store has no runs.
- **`handleRunProject` is async**: returns `{run_id, status:"running"}` immediately; work in goroutine. FE polls `GET .../runs/{id}` ~1.5s.
- **Task results are append-only during run** (`pending` → `running` → `passed|failed|skipped`). FE takes **last status per task name**.
- **On failure, runner continues**: dependents → `skipped`; independent tasks still run. Overall status fails if any task failed.
- **`pipeline_def` JSONB** on each run = historical DAG snapshot. Run detail UI must use this, not live pipeline.
- **Scripts need bash** (`pipefail`); busybox `sh` breaks. Pipeline uses `bash scripts/*.sh`.
- **Prod compose**: `docker/docker-compose.prod.yml`, metaci on **:8081** (dev often :8080).
- **Module path**: `gitlab.com/lunaris-bot/metaci` (not github).

## Conventions
- Conventional commits: `feat:`, `fix:`, `chore:`, `docs:`
- One logical change per commit; push when user workflow expects it
- No secrets in repo; env vars (`METACI_*`, `DATABASE_URL`)
- Do not invent Makefile targets — extend Makefile if needed
- FE hooks: never after early returns (Rules of Hooks)

## Cancel runs
`POST /api/projects/{name}/runs/{id}/cancel` → `PGStore.CancelRun`:
- `running` → `canceled`
- `pending` without incomplete deps → `canceled`
- `pending` depending on incomplete/failed → `skipped`
- `passed` unchanged  
Does **not** kill OS processes yet (status-only); stuck runs still need cancel to leave `running`.

## UI notes
- Vite dev :3000 proxies API to :8080
- Layout drawer is persistent; main content must **not** double-apply `ml: 260` if flex already accounts for drawer — check `Layout.jsx` before changing margins
- DAG node colors: passed green, failed red, running orange, pending/skipped/canceled gray+dimmed

## Verify before done
```bash
go build -o bin/metaci ./cmd/metaci
go test ./... -count=1 -short
# if UI changed:
cd ui && npm run build
```
