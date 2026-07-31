## Why

Remote repository support exists in the acquisition and credential layers, but
the product still registers projects from host-local directories and several
web, webhook, editor, and manual-run paths access `repo_path` directly. Remote
projects therefore lack a coherent onboarding flow and repeatedly clone the
same Git data instead of using a controlled cache.

## What Changes

- **BREAKING**: Make `repo_url` and `repo_ref` mandatory for every registered
  project and remove `repo_path` from project create/update API and UI flows.
- Keep pipeline `ci_repo.path` only as a repository-relative subpath within the
  acquired primary checkout; additional remote repositories continue to use
  `url`, `ref`, and an optional credential reference.
- Replace the local-path project dialog with remote URL, required ref, optional
  non-revoked credential selection/creation, and pipeline-directory fields;
  validate source access and the pipeline before persisting a create or source
  update.
- Add a private, configurable Git object cache with per-source locking,
  authenticated refresh, deterministic ref resolution, bounded retention, and
  isolated per-operation checkouts.
- Route project definition reads, trigger resolution, webhooks, runs, single
  tasks, and manual approvals through one source-session service instead of
  reading registered host paths.
- Reacquire historical manual work from recorded commit SHAs so ref movement or
  later project edits cannot change an existing run.
- Treat remote Git as the pipeline source of truth: pipeline content is
  viewable in Kestrix but not written back by the existing editor/API.
- Remove the non-persisted additional-repository project form and show parsed
  `ci_repo` declarations as read-only pipeline information.
- Require explicit removal or replacement of legacy local-only project rows
  during upgrade; do not reinterpret host paths as remote sources.

## Capabilities

### New Capabilities

- `remote-source-cache`: Securely refresh reusable Git object caches and create
  isolated, commit-pinned source sessions for every consumer.

### Modified Capabilities

- `project-registration`: Registered projects become remote-only and the UI/API
  validate complete source configuration before persistence.
- `pipeline-definition`: Clarify relative subpaths, remote repository fields,
  and read-only pipeline content for remotely registered projects.
- `remote-source-acquisition`: Centralize acquisition across all consumers and
  support exact historical-source rematerialization.
- `webhook-receiver`: Parse pipeline definitions from an acquired remote source
  at the event revision instead of a registered local path.
- `runtime-configuration`: Add validated cache location and retention settings.

## Impact

This affects project models and PostgreSQL migration, project and credential
APIs, React project/editor screens, source adapters, runner/manual-run lifecycle,
webhook handling, runtime configuration, Docker volumes, run metadata,
documentation, and unit/integration/E2E coverage. It builds on
`remote-repository-execution` and `secure-remote-source-credentials`, which must
be integrated first.

## Non-goals

- Provider-specific GitHub, GitLab, or Bitbucket repository APIs and OAuth.
- Writing, committing, or pushing pipeline edits back to a remote repository.
- Sharing private cache data across separate Kestrix installations or tenants.
- Preserving host-local project registrations after the breaking migration.
