## Context

The Kestrix project provides a repo‑agnostic CI/CD control plane. It consists of several major components that need to be documented for developers, ops, and business stakeholders. Existing code includes a Go backend, a React frontend, CI tooling for pipeline execution, and a Terraform provider to integrate Kestrix with infrastructure as code. Additionally, the project includes business‑facing documentation pages (e.g., user guides, marketing material).

## Goals / Non‑Goals

**Goals:**
- Produce a single, coherent documentation set that describes each component of the system.
- Organise the documentation into dedicated sections for Front‑end (FE), Back‑end (BE), CI tools, Terraform provider, and Business pages.
- Ensure the documentation is versioned alongside the code and can be generated automatically as part of the pipeline.

**Non‑Goals:**
- The change does not modify any source code or introduce new functional features.
- It does not create detailed API reference docs; those remain the responsibility of existing tools (e.g., Swagger UI).
- It does not address translation/localisation of the documentation.

## Decisions

- **Single source of truth:** Documentation files will live in the `docs/` directory of the repository and will be generated into the build artifact (`ui/dist` for static site) as part of the CI pipeline.
- **Structure:** Use Markdown files with a clear hierarchy (`docs/overview.md`, `docs/fe.md`, `docs/be.md`, `docs/ci-tools.md`, `docs/terraform-provider.md`, `docs/business.md`).
- **Tooling:** Leverage existing `make docs` target (which runs `swag` for API docs) and extend it with a step to copy the new Markdown files into the static site build.
- **Diagram:** Include an ASCII/Mermaid architecture diagram in the overview file to visualise component interactions.
- **Review process:** The documentation change will be reviewed together with the proposal and task list in a single PR.

## Risks / Trade‑offs

- **Risk:** Documentation may become outdated if the code changes without corresponding doc updates.
  - **Mitigation:** Enforce a CI check that fails if any source file changes without an accompanying doc change (simple `git diff --name-only` hook).
- **Risk:** Over‑documenting low‑impact components could increase maintenance overhead.
  - **Mitigation:** Keep business pages concise and focus on high‑level overviews.

## Migration Plan

1. Add the new Markdown files to the `docs/` folder.
2. Update the `Makefile` to copy these files into the static site during `make ui-build`.
3. Run the CI pipeline to verify that the documentation builds without errors.
4. Deploy the updated site to the documentation server.

## Open Questions

- Should the documentation be versioned per release tag or live on the `main` branch?
- Is a separate documentation site (e.g., Netlify) preferred over serving via the Kestrix server?
