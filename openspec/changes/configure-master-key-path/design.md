## Context

Repository credentials are encrypted in PostgreSQL, while their 256-bit master
key is deliberately held outside the database. The current default provider
tries the desktop `secret-tool` command when YAML `master_key_path` is absent.
That implicit, platform-specific fallback is unsuitable for Kestrix servers and
containers: a deployment can start successfully and only fail later when an
operator creates or uses a credential.

```text
deployment secret / host-provisioned file
                |
                | mounted read-only
                v
KESTRIX_MASTER_KEY_PATH -> startup validation -> FileMasterKeyProvider
                                                    |
                                                    v
                                             credential encryption
                                                    |
                                                    v
                                         PostgreSQL ciphertext only
```

## Goals / Non-Goals

**Goals:**

- Give every runtime a single explicit and portable master-key configuration.
- Reject missing, unreadable, insecure, or malformed keys before serving HTTP.
- Retain compatibility with existing ciphertext when the same key bytes are
  mounted after upgrade.
- Make local and Docker deployment setup clear without depending on a desktop
  session or OS keyring.

**Non-Goals:**

- Key rotation, automatic key generation, recovery, KMS/Vault support, or
  encryption-format changes.
- Moving credential secrets or the master key into configuration files or
  PostgreSQL.

## Decisions

### `KESTRIX_MASTER_KEY_PATH` is the sole configuration interface

The immutable process environment gains a typed `MasterKeyPath` field and the
server receives that value during bootstrap. YAML `master_key_path`, CLI
alternatives, and environment precedence rules are removed for this setting.
Using one source makes container manifests and service managers deterministic.

Alternative considered: retain YAML as a compatibility fallback. Rejected
because it keeps two operational contracts and can silently select a different
key than the deployment intended.

### Validate and construct the provider before opening the HTTP listener

Startup SHALL require a non-empty path and create a `FileMasterKeyProvider`
from it. It SHALL read and validate the file immediately, rather than waiting
for the first credential API call. The file must remain non-group/non-world
readable and contain either 32 raw bytes or 64 hexadecimal characters. Error
messages can identify the configuration variable and path problem, but never
include file contents.

Alternative considered: allow startup and fail only credential operations.
Rejected because it delays an infrastructure error into an operator workflow
and does not prove that a running instance can decrypt existing credentials.

### Remove OS-keyring provider from production wiring

`PGStore` SHALL not supply `OSKeyringProvider` as its default. The keyring
implementation and `secret-tool` invocation are removed unless a test-only
replacement is required. Tests use the existing static provider; production
stores receive the already validated file provider.

Alternative considered: preserve `secret-tool` as a last-resort fallback.
Rejected because fallback behavior is the failure mode this change eliminates
and is unavailable in normal container images.

### Deployment mounts a separately managed file

Compose examples SHALL pass `KESTRIX_MASTER_KEY_PATH` and mount the
host- or secret-manager-provisioned file read-only at that same absolute path
inside the container. No second Kestrix-specific variable is needed. The source
file remains outside the repository and must meet the provider permission
contract. The database volume never contains the master key.

## Risks / Trade-offs

- [Existing deployments do not set the variable] → Fail early with a message
  naming `KESTRIX_MASTER_KEY_PATH` and document the required upgrade step.
- [An operator replaces the key] → Explain that the original key must be
  retained and backed up because existing ciphertext becomes unreadable.
- [Docker mount permissions differ across platforms] → Keep the permission
  contract explicit and cover the documented Compose mount in deployment tests.

## Migration Plan

1. Before upgrading, identify and securely back up the key that encrypted
   existing credential rows.
2. Mount that exact file into the service runtime with restrictive permissions
   and export `KESTRIX_MASTER_KEY_PATH` to its in-container path.
3. Deploy; startup validation verifies that the key can be read and parsed
   before the API becomes available.
4. Roll back by restoring the prior binary while preserving the same external
   key file. No database migration or ciphertext rewrite is required.

## Open Questions

None. The key-file permission contract remains `0600`/no group or other bits,
matching the existing file provider.
