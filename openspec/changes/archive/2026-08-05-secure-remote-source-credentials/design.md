## Context

Remote source acquisition currently accepts a URL and an optional ref, while
credential resolution is environment-backed. This is unsuitable for a
multi-user control plane: credentials need lifecycle management, auditability,
rotation, and a provider-neutral representation without placing plaintext
secrets in PostgreSQL, pipeline HCL, process arguments, logs, or run history.

## Goals / Non-Goals

**Goals:**

- Require `url` and `ref` for every remote `ci_repo`.
- Store credentials in PostgreSQL only as authenticated ciphertext with
  metadata, stable references, revisioned encryption, and revocation state.
- Support GitHub tokens, GitLab tokens, Bitbucket app passwords, generic HTTPS
  username/password, SSH private keys, and known-hosts data through one Git
  credential port.
- Keep secrets write-only through the API and process-scoped during Git use.
- Support local development and container deployments without requiring
  credential values in environment variables.

**Non-Goals:**

- Provider-specific OAuth/API clients or automatic token provisioning.
- Recovering plaintext secrets from the database without the external master
  key.
- A full external secrets manager integration.

## Decisions

### Provider-neutral credential record

PostgreSQL stores a `credential_ref`, display name, credential type, optional
provider/host metadata, encrypted payload, key version, timestamps, and
revocation state. Payloads are normalized JSON before encryption:

- `token`: token plus optional username;
- `basic`: username and password/app password;
- `ssh`: private key and optional known-hosts content.

GitHub PATs and GitLab PATs use `token`; Bitbucket app passwords use `basic`.
The provider field is descriptive and never selects a provider API. Git remains
the only transport integration boundary.

### Encryption and key custody

Use envelope encryption with AES-256-GCM. Each credential gets a random data
encryption key; the payload is encrypted with that key and the data key is
wrapped by a versioned master key. The master key is never stored in
PostgreSQL. Local installations obtain it from the OS keyring; deployments
obtain it from a `0600` mounted secret file such as
`/run/secrets/kestrix_master_key`. Rotation decrypts with the old key and
rewraps with the active key without exposing plaintext through the API.

### Credential API and access boundary

The API creates and rotates credentials with secret-bearing request bodies but
returns only metadata. Reads expose reference, type, host, timestamps, and
revocation status. Delete/revoke is idempotent. Pipeline definitions contain
only `credential_ref`; run history contains no credential payload.

### Acquisition integration

`CredentialStore` resolves a reference to an in-memory credential for the
source adapter. HTTPS credentials use a temporary `GIT_ASKPASS` helper with
`0700` permissions; SSH credentials use temporary files and restrictive
`GIT_SSH_COMMAND` options. Files are removed in a `defer` path. Git errors and
URLs are redacted before persistence or logging.

```text
API secret write
        |
        v
credential normalizer -> envelope encrypt -> PostgreSQL ciphertext
        ^                                  |
        |                                  v
OS keyring / mounted master key    credential_ref lookup
                                           |
                                           v
                                  Git process + cleanup
```

### Remote declaration validation

For `url` repositories, `ref` is mandatory and must be a non-empty branch,
tag, or commit expression accepted by Git. `path` declarations remain the
local-source form. A declaration cannot contain both sources or credentials
embedded in a URL.

## Risks / Trade-offs

- [Master-key loss] → Encrypted credentials become unrecoverable; document
  backup/rotation and fail startup when the key is unavailable.
- [Database compromise] → Ciphertext and metadata may be exposed, but not
  usable secrets without the external master key; use authenticated encryption
  and strict database permissions.
- [Temporary secret leakage] → Restrict file permissions, avoid command-line
  arguments, redact Git output, and test cleanup on failures/timeouts.
- [Provider differences] → Keep provider as metadata and normalize common Git
  transports; defer provider-specific OAuth flows.
- [Breaking remote configs] → Validate before execution and provide a clear
  migration error; local path declarations are unaffected.

## Migration Plan

1. Deploy the credential table, encryption-key provider, and API without
   changing existing local projects.
2. Reject remote declarations lacking `url` or `ref` with an actionable error.
3. Create credentials through the API/UI and replace inline/environment
   references with `credential_ref` values.
4. Enable remote acquisition using encrypted credentials and verify redaction.
5. Roll back application code if needed; retain ciphertext schema for forward
   compatibility. Never downgrade by writing plaintext credentials.

## Open Questions

- Should authorization be restricted by project/user once application-level
  user identities exist?
- Should a later change add an HSM/KMS master-key provider alongside keyring and
  mounted-file providers?
