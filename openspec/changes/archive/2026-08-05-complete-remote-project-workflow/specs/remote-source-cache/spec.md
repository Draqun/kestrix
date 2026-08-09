## ADDED Requirements

### Requirement: Maintain a private reusable Git object cache

Kestrix SHALL cache remote Git objects in private bare repositories keyed by a
non-secret source identity derived from canonical URL and credential reference.
The cache SHALL NOT contain credential payloads or writable execution state.

#### Scenario: First acquisition seeds the cache

- **WHEN** a remote source has no cache entry
- **THEN** Kestrix SHALL authenticate to the remote and seed a bare cache entry
- **AND** SHALL publish the entry atomically only after Git initialization and
  fetch succeed

#### Scenario: Repeated acquisition reuses Git objects

- **WHEN** a valid cache entry already exists for the same source identity
- **THEN** Kestrix SHALL refresh that entry instead of transferring a complete
  repository into a new cache
- **AND** SHALL not reuse writable files from an earlier source session

#### Scenario: Cache identity contains no secret

- **WHEN** Kestrix derives an entry path or writes cache metadata
- **THEN** the path and metadata SHALL omit tokens, passwords, private keys,
  credential payloads, and URL passwords or tokens

### Requirement: Refresh and resolve remote refs deterministically

Kestrix SHALL refresh a cache entry from its configured remote before resolving
a requested branch, tag, or commit and SHALL return an immutable commit SHA.
Failure to refresh a mutable ref SHALL NOT silently use stale cached state.

#### Scenario: Mutable branch advances

- **WHEN** a cached branch points to an older commit than its remote
- **THEN** Kestrix SHALL fetch the remote update under the entry lock
- **AND** SHALL materialize and report the newly resolved commit SHA

#### Scenario: Remote refresh fails

- **WHEN** authentication, transport, timeout, or ref resolution fails
- **THEN** source acquisition SHALL fail with a redacted actionable error
- **AND** SHALL not execute against the previously cached branch value

### Requirement: Isolate sessions and serialize mirror mutation

Each source consumer SHALL receive a distinct private checkout pinned to its
resolved SHA. Cache mutation SHALL be serialized per entry while independent
entries and read-only materialization MAY proceed concurrently.

#### Scenario: Concurrent acquisition of one source

- **WHEN** two operations refresh the same source concurrently
- **THEN** Kestrix SHALL prevent concurrent mutation from corrupting the mirror
- **AND** each operation SHALL receive a separate checkout directory

#### Scenario: Session modifies source files

- **WHEN** a task changes tracked or untracked files in its checkout
- **THEN** those changes SHALL not alter the mirror or another operation's
  checkout
- **AND** session cleanup SHALL remove the changed workspace

### Requirement: Enforce cache authorization, integrity, and retention

A cache hit SHALL NOT bypass current credential resolution or authenticated
remote refresh. Kestrix SHALL detect corrupt entries, rebuild them at most once
per acquisition, and evict only unlocked, unpinned entries according to
configured age and size limits.

#### Scenario: Credential is revoked after cache population

- **WHEN** a private source's credential has been revoked
- **THEN** a later acquisition SHALL fail before cached content is materialized
- **AND** SHALL not expose whether particular private objects exist in the cache

#### Scenario: Cache entry is corrupt

- **WHEN** Git validation or refresh identifies a corrupt mirror
- **THEN** Kestrix SHALL quarantine and rebuild the entry once under its lock
- **AND** SHALL fail cleanly if rebuilding does not succeed

#### Scenario: Retention reaches a pinned entry

- **WHEN** cache maintenance encounters an entry containing a commit pinned by
  a run that can still execute work
- **THEN** it SHALL retain that entry regardless of normal age or size eviction
