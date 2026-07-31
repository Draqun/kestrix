## 1. Model and declaration parsing

- [x] 1.1 Extend the repository domain model with source kind, remote URL, requested ref, and credential reference while preserving local-path repositories.
- [x] 1.2 Update HCL parsing and validation so a repository declares exactly one of `path` or `url`, validates optional `ref` and `credential_ref`, and rejects credentials embedded in URLs.
- [x] 1.3 Add parser and validation tests for local repositories, public remote repositories, private remote declarations, and invalid/malformed declarations.

## 2. Source acquisition ports and adapters

- [x] 2.1 Introduce a provider-neutral `SourceAcquirer` port and acquisition result containing the isolated workspace path plus requested and resolved revisions.
- [x] 2.2 Implement the local-path adapter using the existing repository behavior, including monorepo subdirectory resolution and clear errors for missing paths.
- [x] 2.3 Implement the Git CLI adapter for standard HTTPS and SSH URLs on arbitrary Git hosts, including clone, checkout, ref resolution, and immutable commit pinning.
- [x] 2.4 Add adapter tests using a temporary local bare Git repository and a fake Git executable; cover branch, tag, commit, missing ref, clone failure, and command-timeout errors.

## 3. Credentials and secure process handling

- [x] 3.1 Define a credential resolver port and configuration for credential references backed by environment variables, Git credential helpers, and the SSH agent.
- [x] 3.2 Implement process-scoped credential injection without persisting secrets in project definitions, run metadata, command arguments, or logs; redact source URLs and Git errors.
- [x] 3.3 Add tests for successful private-source resolution, missing credentials, embedded-credential rejection, redaction, and cleanup of temporary credential material.

## 4. Runner integration and workspace lifecycle

- [x] 4.1 Create a per-run `0700` workspace and acquire all declared sources before the DAG starts; map each logical repository name to its acquired directory.
- [x] 4.2 Pass acquired repository directories to existing local, Docker, and GitLab-runner executors without changing task scripts or local execution semantics.
- [x] 4.3 Add bounded acquisition retries, clone/checkout timeouts, and cleanup on success, failure, cancellation, and timeout, with an explicit debug-retention option if supported.
- [x] 4.4 Add integration tests proving that acquisition failure prevents task execution and that concurrent runs do not share source workspaces.

## 5. Persistence, API, and operational configuration

- [x] 5.1 Persist and expose the source declaration and resolved commit SHA in run context/history while excluding all secret values.
- [x] 5.2 Update project registration, API, configuration loading, and UI/forms as needed to accept provider-neutral remote source fields and credential references.
- [x] 5.3 Add documentation and examples for public and private repositories hosted by GitHub, GitLab, Bitbucket, self-hosted Git, and arbitrary Git servers using standard Git URLs.

## 6. Verification

- [x] 6.1 Run formatting, vet/static analysis, unit/integration tests, and the project build; fix regressions in existing local-path pipeline execution.
- [x] 6.2 Run strict OpenSpec validation for `remote-repository-execution` and record the implemented behavior against every requirement scenario.
