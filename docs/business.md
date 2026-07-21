# Business Pages

This section contains public‑facing documentation and business‑oriented content about the Kestrix platform.

## Overview

Kestrix is a repo‑agnostic CI/CD control plane designed to decouple pipeline logic from vendor‑specific CI config files. Teams define pipelines in HCL using a common syntax and execute them locally, in Docker, or on GitLab runners.

## Target Audience

- **Platform engineers** — managing shared CI/CD infrastructure across teams
- **DevOps teams** — migrating from vendor‑locked CI to a portable pipeline definition
- **Open‑source contributors** — extending Kestrix with new runners, triggers, and integrations

## Benefits

- Write pipelines once, run anywhere (local, Docker, GitLab Runner)
- HCL-based definitions are readable, versionable, and reviewable
- Trigger expressions enable fine‑grained dependency ordering
- Paginated API and UI scale to hundreds of projects and thousands of runs

## Getting Started

See the [Usage Guide](usage.md) for setup instructions and [Architecture](architecture.md) for a high‑level system overview.
