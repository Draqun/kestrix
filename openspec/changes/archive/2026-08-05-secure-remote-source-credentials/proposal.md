## Why

Remote repository execution currently permits declarations without a URL or
revision and can resolve credentials from process environment variables. This
makes source selection non-deterministic and leaves credential management
outside Kestrix. The next iteration should make remote sources explicit and
provide a provider-neutral, database-backed credential lifecycle for GitHub,
GitLab, Bitbucket, self-hosted Git, and arbitrary Git servers.

## What Changes

- **BREAKING**: Require every remote repository declaration to provide both
  `url` and `ref`; existing incompatible declarations may be removed.
- Reject missing, mutable-default, or embedded-credential remote declarations.
- Add encrypted credential records in PostgreSQL referenced by a stable
  `credential_ref`; never store plaintext secrets, URLs with secrets, or
  credentials in run metadata and logs.
- Support provider-neutral credential types: HTTPS token, username/password or
  app password, SSH private key, and SSH known-hosts material.
- Add credential create, rotate, inspect-metadata, and revoke operations with
  secret write-only responses.
- Resolve credentials at acquisition time and inject them only into the Git
  process; remove temporary material after acquisition.
- Preserve standard Git transport rather than adding provider-specific API
  clients or OAuth integrations.

## Capabilities

### New Capabilities

- `repository-credentials`: Encrypted, provider-neutral credential storage,
  resolution, rotation, revocation, and redaction.

### Modified Capabilities

- `pipeline-definition`: Remote repositories require `url` and `ref`.
- `project-registration`: Projects and repository forms accept credential
  references without accepting secret values.

## Impact

- Domain model, HCL validation, source acquisition, and Git process handling.
- PostgreSQL schema, migrations, API handlers, UI forms, and run metadata.
- Key management integration using the OS keyring in local mode or a mounted
  master-key file in deployments; the encryption key is never stored in the
  database.
- Documentation, examples, and migration guidance for breaking declarations.

## Non-goals

- Provider-specific OAuth/API clients.
- Storing plaintext secrets in PostgreSQL or environment variables.
- Automatic migration of invalid existing remote declarations.
- A full external secrets-manager integration in this change.
