## Context

The current runner receives a local working directory and passes repository subdirectories to task executors. A remote repository must be fetched before the DAG starts, while credentials must remain outside HCL, logs, and persistent run data. The acquisition mechanism should not depend on GitHub, GitLab, or Bitbucket APIs.

## Decisions

### Provider-neutral domain port

Introduce a `SourceAcquirer` port. It accepts a source declaration and run context, then returns an isolated directory and source metadata. The first implementation uses the installed `git` CLI, which supports standard HTTPS and SSH transports and arbitrary Git hosts. A future implementation can replace it without changing pipeline semantics.

The source declaration contains:

- `url` for a remote Git source;
- optional `ref` (branch, tag, or commit; the default is the provider's default branch);
- optional `credential_ref`, which names a configured credential without containing its value.

Local repositories continue to use `path`. A declaration must provide exactly one of `path` or `url`.

### Acquisition lifecycle

All sources are acquired before any task is started. Each run gets a `0700` temporary workspace and each logical repository maps to a separate directory. Existing executors receive the acquired path through the same repository workspace abstraction used for local sources.

The Git adapter clones without credentials in the URL, checks out the requested ref, and resolves `HEAD` to a commit SHA. The requested ref and resolved SHA are retained in run metadata so a run can be audited and reproduced. Missing refs and clone failures fail acquisition before task execution.

Acquisition has bounded retries and a timeout. Workspace cleanup happens on success, failure, cancellation, and timeout; optional debug retention must be explicit.

```text
pipeline declaration
        |
        v
source validation --> credential resolution (process scoped)
        |
        v
per-run workspace --> local path adapter / Git CLI adapter
        |
        v
resolved repository paths + commit SHAs
        |
        v
DAG execution (local / Docker / GitLab runner)
        |
        v
run metadata + workspace cleanup
```

### Credentials

Credential resolution is a separate port. Supported initial sources are environment-backed references, the user's configured Git credential helper, and an SSH agent. Secrets are injected only into the Git process environment or temporary files with restrictive permissions, then removed. They are never written to HCL, URLs, command arguments, persistent configuration, or logs.

The adapter rejects embedded credentials in URLs and redacts source URLs and Git errors before reporting them. SSH host verification follows the configured Git/SSH policy; disabling verification is not an implicit default.

### Compatibility and migration

Existing `path` declarations continue to work unchanged. Remote declarations are opt-in. Project registration and API consumers can expose `url`, `ref`, and `credential_ref`, while responses and run history expose only non-secret source metadata.

## Risks and mitigations

- Git availability differs between runner images. Validate the dependency at startup/acquisition and report an actionable error.
- Mutable branches can change between runs. Record the resolved SHA and allow commit pinning.
- A failed or canceled clone can leave data behind. Tie cleanup to the run lifecycle and enforce workspace permissions.
- Credentials can leak through Git diagnostics. Centralize redaction and test representative failures.
- Cloning every run can be slow. Defer caching until isolation, invalidation, and credential boundaries are specified.

## Open questions

- Should a later change add a content-addressed cache, and what sharing policy is acceptable for private sources?
- Which exact environment variable naming convention should credential references use for the first release?
