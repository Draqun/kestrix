## Why

Kestrix currently executes pipelines against repositories that already exist on the local filesystem. Although project registration has a repository URL field, the pipeline runner has no provider-neutral source acquisition step, revision handling, or secure credential flow. This prevents deployments from public or private repositories hosted by GitHub, GitLab, Bitbucket, self-hosted services, or arbitrary Git servers.

## What Changes

- Add a provider-neutral source acquisition layer before DAG task execution.
- Support standard Git HTTPS and SSH URLs on arbitrary hosts, without provider-specific API clients.
- Support branch, tag, and commit revisions, resolving mutable refs to an immutable commit SHA for each run.
- Add secure credential references using environment-backed credentials, Git credential helpers, and SSH agent support; never store secrets in pipeline definitions or URLs.
- Create isolated per-run source workspaces and clean them up on completion, failure, cancellation, or timeout.
- Extend repository declarations, registration, persistence, API, and run metadata with source and resolved-revision information.
- Preserve local-path repositories and existing executor behavior.

## Capabilities

### New Capabilities

- `remote-source-acquisition`: Acquire public or private Git sources in an isolated run workspace with deterministic revision and secure authentication handling.

### Modified Capabilities

- `pipeline-definition`: A repository may be a local path or a remote Git URL with an optional revision and credential reference.

## Impact

- Domain model, HCL parser, and validation.
- Runner orchestration and local, Docker, and GitLab-runner executors.
- Project registration, persistence, API/UI configuration, and run history metadata.
- Runtime configuration and secret handling.
- Documentation and examples.

No provider-specific OAuth, webhook, or repository-management API is required for this change. Standard Git transport remains the integration boundary.

## Non-goals

- Replacing the existing local-path execution mode.
- Implementing provider-specific API clients or OAuth flows.
- Introducing a complete secrets-manager integration.
- Implementing source caching before workspace isolation and correctness are established.
