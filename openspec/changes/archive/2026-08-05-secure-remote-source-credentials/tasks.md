## 1. Remote declaration contract

- [x] 1.1 Update the repository domain model and HCL parser so remote declarations require non-empty `url` and `ref`, while local `path` declarations remain supported.
- [x] 1.2 Reject embedded credentials and incomplete remote project registration with actionable redacted errors.
- [x] 1.3 Add parser, project-service, and API tests for valid refs, missing refs, invalid source combinations, and legacy configurations that must be migrated or removed.

## 2. Key custody and encryption

- [x] 2.1 Define a master-key provider port and implement OS-keyring and `0600` mounted-file providers without reading credential values from environment variables.
- [x] 2.2 Implement versioned AES-256-GCM envelope encryption with authenticated metadata, random per-record data keys, decrypt, rewrap, and key-unavailable errors.
- [x] 2.3 Add crypto tests for round trips, tampering, wrong keys, key rotation, and ensuring plaintext is absent from serialized records and logs.

## 3. Credential storage and lifecycle

- [x] 3.1 Add PostgreSQL credential tables, migrations, indexes, revocation state, key version, timestamps, and repository/project reference constraints.
- [x] 3.2 Implement provider-neutral credential normalization for token, basic/app-password, and SSH private-key/known-hosts forms.
- [x] 3.3 Implement create, rotate, metadata-read, and revoke service operations with write-only secret handling and audit-safe errors.
- [x] 3.4 Add HTTP API endpoints and UI forms for credential lifecycle operations; never return secret fields in responses.
- [x] 3.5 Add persistence and API tests proving ciphertext storage, revocation, rotation, and secret omission.

## 4. Source acquisition integration

- [x] 4.1 Replace environment credential lookup with the credential-store resolver and preserve the `credential_ref` boundary through run execution.
- [x] 4.2 Inject HTTPS and SSH credentials only into Git child processes using restrictive temporary material, cleanup hooks, and redaction.
- [x] 4.3 Add acquisition tests for GitHub/GitLab tokens, Bitbucket app passwords, generic hosts, SSH keys, missing/revoked credentials, timeout cleanup, and redacted diagnostics.

## 5. Migration and documentation

- [x] 5.1 Add migration diagnostics and documentation for removing or updating existing remote declarations without `url` and `ref`.
- [x] 5.2 Document provider-neutral credential setup, master-key backup/rotation, API usage, and examples for GitHub, GitLab, Bitbucket, self-hosted Git, and arbitrary Git servers.

## 6. Verification

- [x] 6.1 Run formatting, vet, static analysis, unit/integration tests, UI build, and E2E checks; fix regressions in local and remote execution.
- [x] 6.2 Run strict OpenSpec validation and verify every credential and required-ref scenario against tests.
