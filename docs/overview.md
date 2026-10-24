# Project Overview

Kestrix is a repo‑agnostic CI/CD control plane that turns HCL pipelines into executable DAGs, resolves triggers, and runs jobs locally, in Docker, or on GitLab runners. It provides a webhook server, a flexible DAG engine, a CI generator, and a dispatch client.

## Core Concepts
- **HCL pipelines** define projects, repositories, and tasks.
- **[DAG engine](dag-engine.md)** resolves task order based on trigger expressions.
- **CI generator** produces `gitlab-ci.yml` files.
- **Dispatch client** triggers GitLab pipelines.
- **Webhook receiver** handles push events and initiates the pipeline.

## Project Components

| Component | Description |
|-----------|-------------|
| [Frontend](fe.md) | React SPA with MUI, pipeline editor, DAG viewer |
| [Backend](be.md) | Go HTTP server, parser, DAG engine, runner |
| [CI Tools](ci-tools.md) | CI generator, dispatch client, runner modes |
| [Terraform Provider](terraform-provider.md) | Infrastructure‑as‑code for project management, maintained in a separate repository |
| [Business Pages](business.md) | Public‑facing documentation and onboarding |
| [Authentication](authentication.md) | Login, roles, project grants, and account recovery |

## See Also
- [DAG engine documentation](dag-engine.md)
- [Usage guide](usage.md)
- [Authentication](authentication.md)
- [Architecture overview](architecture.md)
- [Remote runners scaling](remote-runner-scaling.md)
- [Artifact storage architecture](artifact-storage-architecture.md)

