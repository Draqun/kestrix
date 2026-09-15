# Architecture

The system consists of several loosely coupled components:

- **Parser** – reads HCL files (`pipeline.tf`) and builds a `model.Project`.
- **[DAG Engine](dag-engine.md)** – evaluates boolean trigger expressions, detects cycles, and orders tasks.
- **CI Generator** – converts the resolved task set into a `gitlab‑ci.yml`.
- **Dispatch** – uses the GitLab API to start a pipeline.
- **Webhook Receiver** – accepts GitLab push events, extracts changed repos, and triggers the resolution flow.
- **Runner** – executes tasks locally (default) or via Docker/GitLab‑Runner.
- **Store** – persists project metadata and run history in PostgreSQL.

These components communicate via Go interfaces and share configuration via the startup environment snapshot (`KESTRIX_*`).

## Single-instance constraint

Exactly one `serve` process is supported per database. A run executes inside its
process's own goroutines (`RunService.Start`), not in the database, so a second
process sharing the same database would misclassify the first one's live runs as
interrupted. Multi-instance coordination is not implemented.

On startup, before the HTTP listener opens, Kestrix fails every run still
`running` from a previous instance (crash, redeploy, `docker compose up -d`):
each such run is marked `failed` with error `interrupted by server restart`,
its non-terminal task results are marked `failed` (was `running`) or `skipped`
(was `pending`), and the run's output gains a trailing line naming the
interruption. A run only waiting on manual approval is left alone - it remains
`running` and approvable. Interrupted tasks are not resumed or retried; the
operator re-runs explicitly.
