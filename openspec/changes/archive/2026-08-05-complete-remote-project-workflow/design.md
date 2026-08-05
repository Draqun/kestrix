## Context

The two prerequisite changes added remote `ci_repo` acquisition and encrypted
credential storage, but project registration still treats a host checkout as a
first-class source. The create dialog only submits `repo_path`; update permits
mutually exclusive local and remote fields at the same time; additional repos
are edited in a project-only structure that PostgreSQL does not persist; and
the pipeline save endpoint assumes a writable local checkout.

Remote source use is also fragmented. Definition and run handlers create
temporary clones, while webhook and manual-approval paths still dereference
`ProjectConfig.RepoPath`. Every read can transfer the full repository again,
and a manual task can execute against a branch different from the commit that
created its run.

## Goals / Non-Goals

**Goals:**

- Make a remote URL and explicit ref the only registered-project source form.
- Give every source consumer one secure, deterministic acquisition boundary.
- Reuse Git objects without sharing writable worktrees between operations.
- Keep credential checks, redaction, and cleanup effective on cache hits.
- Preserve exact source commits while a run can still execute manual work.
- Make project onboarding and pipeline UI match the remote-only contract.

**Non-Goals:**

- Provider-specific APIs, OAuth, repository creation, or webhook registration.
- Editing, committing, or pushing remote pipeline files from Kestrix.
- A distributed cache shared by multiple installations or future tenants.
- Compatibility for registered host-local projects.

## Decisions

### Registered projects are remote; pipeline paths are relative

`ProjectConfig` will contain `RepoURL`, `RepoRef`, optional
`RepoCredentialRef`, and the relative pipeline location, but no runtime
`RepoPath`. API DTOs reject `repo_path`. The database column is removed only
after an explicit preflight verifies that no local-only rows remain; operators
must update or delete those rows before upgrade.

This does not remove `ci_repo.path`. In pipeline HCL, `path` means a clean,
relative subdirectory of the acquired primary checkout and cannot be absolute
or escape its root. Additional Git repositories use `url` plus required `ref`.
This keeps monorepos without retaining arbitrary server filesystem access.

Alternative considered: keep a local/remote selector. It preserves the current
UI but also preserves deployment-specific paths and two source lifecycles, so
it is rejected for registered projects. The CLI may continue to run an
explicit local pipeline file as a separate developer workflow.

### A source-session service owns all repository materialization

Introduce a domain port such as `SourceSessionManager.Open`. A request contains
the normalized URL, requested ref or exact SHA, credential reference, purpose,
and optional run identity. The result contains a private checkout root,
requested ref, resolved SHA, and idempotent cleanup function.

Project create/update validation, raw and parsed definition reads, trigger
resolution, webhook handling, full and single-task runs, and manual approvals
all use this port. No web handler reads a registered filesystem path directly.
The runner uses the same manager for additional remote `ci_repo` declarations.

```text
Project UI / API / webhook / run service
                    |
                    v
          SourceSessionManager
          /        |          \
 credential   Git mirror       run-source pins
 resolver       cache          (exact SHA)
          \        |          /
           fetch + resolve SHA
                    |
                    v
          isolated 0700 checkout
                    |
       parse / resolve / execute / cleanup
```

The service receives request contexts and configured timeouts. Session cleanup
is owned by the caller and is guaranteed on success, failure, cancellation, and
panic-safe deferred paths.

### Cache private bare mirrors, never writable workspaces

The cache stores one bare mirror per SHA-256 key derived from canonical URL and
credential reference. Neither secrets nor credential payloads occur in cache
keys, paths, Git configuration, or metadata. Including the credential reference
prevents accidental object sharing across authentication scopes; rotating a
credential retains its cache identity while using the new secret on the next
refresh.

An entry is seeded in a temporary sibling directory and atomically renamed.
Every refresh takes a per-entry advisory lock, runs an authenticated forced
fetch with pruning, validates the requested ref, and resolves it to a commit.
Callers never silently fall back to a stale mutable ref when refresh fails.
Corrupt entries are quarantined and rebuilt once under the same lock.

Each operation clones locally from the mirror into its own `0700` session and
checks out the resolved SHA detached. It does not use a shared Git worktree or
writable cache files. This costs some local I/O but gives straightforward
concurrency and cleanup semantics.

Alternative considered: preserve complete clones between runs. Shared indexes,
worktrees, ignored files, and task mutations make isolation unreliable, so only
immutable Git objects are reused.

### Cache hits never bypass authorization

For a source with `credential_ref`, every open operation first resolves a
current, non-revoked database credential and performs the authenticated remote
refresh before materializing cached content. Revocation, missing key custody,
or remote authorization failure therefore fails closed even if objects exist
locally. Temporary HTTPS/SSH material remains process-scoped and is removed
after Git exits. Errors and URLs pass through centralized redaction.

The cache root and entries are private to the Kestrix service account. Cache
metadata contains only the canonical non-secret URL, credential reference,
timestamps, size, and resolved object identifiers. API responses expose no
cache filesystem paths.

### Pin commits needed by incomplete runs

When a run records resolved sources, the cache creates internal refs for commits
needed by pending or manual tasks. Cache pruning and eviction skip pinned
entries. Manual approval opens sessions from the run's pipeline snapshot and
recorded SHAs, never from the project's current ref. Pins are released when no
task in that run can execute again; normal retention then applies.

Run source metadata stores the non-secret source identity needed for this
operation: logical repository name, canonical URL, requested ref, resolved SHA,
and credential reference/cache identity. It never stores credential payloads or
checkout/cache paths. This includes the registered primary repository, not only
the `ci_repo` blocks parsed from its pipeline.

Webhook acquisition requests the immutable checkout SHA from the push payload
for the registered source. If it cannot be fetched or proven present, the
webhook fails without resolving tasks from a different revision.

### Validate onboarding before persistence

The create/edit dialog collects project name, repository URL, required ref,
optional active credential selected from credential metadata, and pipeline
directory. Submitting opens a source session and parses all pipeline `.tf`
files before the transaction writes the project. An update validates the full
replacement source configuration, allowing an optional credential to be
cleared explicitly. Failure leaves the previous record unchanged and returns a
redacted, field-oriented error.

Credential management is available without selecting an existing project. The
onboarding dialog can open the write-only credential creation flow, refresh the
metadata list, and select the new reference, allowing a fresh installation to
register its first private project.

Additional repositories are defined only by `ci_repo` blocks and displayed
read-only in project detail. The pipeline viewer remains available, while save
controls and the remote pipeline write endpoint are disabled because Git is the
source of truth.

### Bound and configure cache storage

Configuration gains typed source-cache directory, maximum age, and maximum
bytes settings with the existing CLI > config file > canonical environment >
default precedence. Local default storage is below `~/.kestrix/cache/git`;
containers use a persistent `/var/lib/kestrix/cache/git` volume. Values must be
non-empty/positive and the directory must be creatable with mode `0700`.

Best-effort maintenance runs after successful acquisition and at startup:
unpinned entries older than the age limit are removed first, followed by least
recently used unpinned entries until the size limit is met. Maintenance cannot
remove locked or pinned entries and does not block an unrelated cache key.

## Risks / Trade-offs

- **[Private source content remains on disk]** → Use a dedicated `0700` cache,
  scope keys by credential reference, document volume protection, and never
  expose cache paths through APIs.
- **[Forced fetch or pruning races with execution]** → Lock mirror mutation and
  materialize an isolated checkout at an already resolved SHA before release.
- **[Cache growth exhausts disk]** → Enforce age/size retention, track last use,
  and protect only entries with active pins.
- **[Remote outage removes former implicit offline behavior]** → Fail closed
  instead of using stale mutable refs; exact run commits remain pinned but still
  require a valid credential boundary.
- **[Breaking local-project migration blocks startup]** → Add a checked
  preflight with project names and documented update/delete commands before the
  schema constraint is applied.
- **[Webhook SHA is unavailable after force-push]** → Return a redacted source
  acquisition error and do not evaluate another commit.

## Migration Plan

1. Integrate and archive `remote-repository-execution`, then
   `secure-remote-source-credentials`, so their capabilities are baseline.
2. Add source-cache configuration, persistent container volume, mirror store,
   source-session service, and checked migration code without changing API
   fields yet.
3. Route every project, webhook, run, and manual consumer through source
   sessions; add run pins and exact-SHA tests.
4. Replace project UI/API DTOs, make remote pipeline content read-only, and
   remove the non-persisted additional-repository editor.
5. Run a migration preflight. Explicitly update/delete local-only projects,
   then apply the non-null remote-source constraints and remove `repo_path`.
6. Verify unit, PostgreSQL integration, Git integration, UI, and deployment E2E
   suites, including cache concurrency, corruption, revocation, and restart.

Rollback requires restoring the previous binary and schema backup. Cache data
is disposable and may be removed independently; encrypted credentials and Git
source data must never be copied into a legacy plaintext path.

## Open Questions

- Future user/tenant authorization will require tenant identity in cache scope;
  cross-tenant sharing remains forbidden until that model exists.
