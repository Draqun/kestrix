## 1. Remote-only project contract and migration

- [x] 1.1 Add service and HTTP tests proving project create/update require
  `repo_url` and `repo_ref`, reject `repo_path`, reject embedded credentials,
  and allow an optional credential reference to be explicitly cleared.
- [x] 1.2 Add PostgreSQL integration tests for remote-only project persistence,
  non-null URL/ref constraints, and migration preflight diagnostics for named
  local-only rows.
- [x] 1.3 Replace project request/domain/store fields that expose `RepoPath`
  with the remote-only source contract while retaining local path arguments
  only in explicit CLI execution and transient source-session results.
- [x] 1.4 Implement a checked, transactional schema migration that refuses
  local-only rows before removing `repo_path` and applying required URL/ref
  constraints; stop discarding migration statement errors.
- [x] 1.5 Make project updates full source replacements with atomic validation
  and persistence so empty optional values are not restored from the old row.

## 2. Cache configuration and deployment storage

- [x] 2.1 Add failing configuration tests for source-cache directory, maximum
  age, maximum bytes, canonical environment names, CLI/file/environment/default
  precedence, and invalid values.
- [x] 2.2 Add typed source-cache settings to config, immutable environment,
  `serve` flags, web wiring, defaults, and redacted startup diagnostics.
- [x] 2.3 Add tests for private cache-directory creation and refusal of an
  unusable or incorrectly typed cache location.
- [x] 2.4 Add a persistent production Compose cache volume and isolated
  temporary workspace configuration without hardcoded host paths.

## 3. Git mirror cache

- [x] 3.1 Add Git integration tests for atomic first seed, repeat refresh,
  branch movement, tag and commit resolution, and failure without stale-ref
  fallback.
- [x] 3.2 Add security tests proving cache keys, paths, metadata, Git config,
  errors, and process arguments contain no credential payload or embedded URL
  secret.
- [x] 3.3 Implement canonical non-secret source identity and private bare-mirror
  entry layout keyed by URL plus credential reference.
- [x] 3.4 Implement atomic mirror seeding, authenticated forced fetch/prune,
  requested-ref validation, and immutable SHA resolution through the Git CLI.
- [x] 3.5 Add concurrency tests for per-entry locking, independent-entry
  progress, canceled waiters, and two isolated checkouts of the same SHA.
- [x] 3.6 Implement per-entry advisory locking and detached private checkout
  materialization without shared writable worktree state.
- [x] 3.7 Add corruption tests and implement one-time quarantine/rebuild under
  lock while preserving a clear error after a second failure.

## 4. Cache authorization, pins, and retention

- [x] 4.1 Add tests proving a cached private source still resolves the current
  encrypted credential and fails closed after revocation, missing key custody,
  or remote authentication failure.
- [x] 4.2 Integrate database credential resolution and temporary HTTPS/SSH
  process material into every cache refresh and keep cleanup/redaction on all
  exits.
- [x] 4.3 Add tests for run commit pin creation, restart recovery, retention
  exclusion, terminal-run release, and cleanup of stale released pins.
- [x] 4.4 Implement internal cache refs and persisted pin ownership needed to
  protect source commits while a run can execute pending or manual work,
  including restart-safe primary/additional source identity without secrets.
- [x] 4.5 Add deterministic retention tests for maximum age, maximum bytes,
  least-recently-used order, locked entries, pinned entries, and filesystem
  failures.
- [x] 4.6 Implement bounded startup/post-acquisition maintenance that evicts
  only unlocked and unpinned entries and never blocks unrelated source keys.

## 5. Central source-session integration

- [x] 5.1 Add domain/service tests for a `SourceSessionManager` request/result
  contract, idempotent cleanup, context timeout, exact-SHA requests, and source
  metadata without cache paths.
- [x] 5.2 Implement the source-session port and adapter over credential
  resolution, mirror refresh, checkout materialization, and run pinning.
- [x] 5.3 Add project endpoint tests proving create/update validation, raw and
  parsed definition reads, and trigger resolution all use source sessions and
  clean them on success and failure.
- [x] 5.4 Route project create/update validation and every project-definition
  consumer through source sessions; remove direct registered-path access from
  web handlers.
- [x] 5.5 Add parser and runner tests that `ci_repo.path` is relative, cannot
  escape the primary checkout, and additional remote repos use the same source
  session manager.
- [x] 5.6 Update parser and runner source preparation to enforce the clarified
  subpath semantics and to persist each requested ref and resolved SHA.

## 6. Run, manual approval, and webhook correctness

- [x] 6.1 Add run-service integration tests proving full and single-task runs
  use one acquired primary revision and acquire all additional sources before
  any task starts.
- [x] 6.2 Add a regression test where a branch moves before manual approval and
  prove the approved task uses the stored pipeline snapshot and recorded source
  SHAs rather than current project configuration.
- [x] 6.3 Refactor manual approval to rematerialize exact run sources, preserve
  pin ownership while work remains, and release pins when the run is terminal.
- [x] 6.4 Extend GitLab push parsing tests with immutable checkout SHA and add
  webhook tests proving pipeline loading uses that exact source revision.
- [x] 6.5 Route webhook pipeline loading through the source-session manager and
  fail without task resolution or dispatch when the event revision cannot be
  acquired.
- [x] 6.6 Add cancellation, timeout, acquisition failure, and concurrent-run
  tests proving no task starts early and every temporary workspace is cleaned.

## 7. Remote project and pipeline UI

- [x] 7.1 Add frontend tests for a remote-only new/edit project form with
  required URL/ref, pipeline directory, active credential selection, clearable
  optional credential, pre-project credential creation, and redacted field
  errors.
- [x] 7.2 Replace local-path project form state and payloads with the remote
  fields, make credential management available before any project exists, and
  refresh project detail only after server-side source validation succeeds.
- [x] 7.3 Remove the non-persisted additional-repository editor and display
  parsed local-subpath/remote `ci_repo` metadata read-only without secrets or
  cache paths.
- [x] 7.4 Add frontend/API tests for read-only remote pipeline content, remove
  save controls for registered projects, and make the write endpoint return an
  explicit unsupported-operation response.
- [x] 7.5 Update project list/detail labels to show URL, requested ref, resolved
  revision where available, credential metadata, and source status without
  presenting a host filesystem path.

## 8. Documentation and end-to-end verification

- [x] 8.1 Document remote-only project onboarding, public/private credential
  selection, pipeline subpath semantics, Git-as-source-of-truth editing, cache
  settings, volume protection, retention, and troubleshooting.
- [x] 8.2 Document the breaking local-project preflight and explicit
  replace/delete procedure, backup/rollback expectations, and prerequisite
  archive order for the two earlier remote-source changes.
- [x] 8.3 Add backend and UI E2E coverage that creates a project from a remote
  bare Git repository, reuses the cache after a branch update, runs its
  pipeline, and rejects local-path registration.
- [x] 8.4 Extend deployment E2E to verify cache persistence across a Kestrix
  container restart, credential revocation behavior, and absence of secrets in
  logs and API responses.
- [x] 8.5 Run Makefile-provided formatting, static analysis, unit, PostgreSQL
  integration, Git integration, UI build/test, application build, and all E2E
  targets; fix regressions.
- [x] 8.6 Run strict OpenSpec validation and reconcile every requirement
  scenario with an automated test or an explicit operational verification.
