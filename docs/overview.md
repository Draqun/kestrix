# Project Overview

Kestrix is a repo‑agnostic CI/CD control plane that turns HCL pipelines into executable DAGs, resolves triggers, and runs jobs locally, in Docker, or on GitLab runners. It provides a webhook server, a flexible DAG engine, a CI generator, and a dispatch client.

## Core Concepts
- **HCL pipelines** define projects, repositories, and tasks.
- **DAG engine** resolves task order based on trigger expressions.
- **CI generator** produces `gitlab-ci.yml` files.
- **Dispatch client** triggers GitLab pipelines.
- **Webhook receiver** handles push events and initiates the pipeline.
