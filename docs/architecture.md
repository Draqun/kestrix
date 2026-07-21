# Architecture

The system consists of several loosely coupled components:

- **Parser** – reads HCL files (`pipeline.tf`) and builds a `model.Project`.
- **[DAG Engine](dag-engine.md)** – evaluates boolean trigger expressions, detects cycles, and orders tasks.
- **CI Generator** – converts the resolved task set into a `gitlab‑ci.yml`.
- **Dispatch** – uses the GitLab API to start a pipeline.
- **Webhook Receiver** – accepts GitLab push events, extracts changed repos, and triggers the resolution flow.
- **Runner** – executes tasks locally (default) or via Docker/GitLab‑Runner.
- **Store** – persists project metadata in PostgreSQL (or a JSON file in dev).

These components communicate via Go interfaces and share configuration via environment variables (`METACI_*`).
