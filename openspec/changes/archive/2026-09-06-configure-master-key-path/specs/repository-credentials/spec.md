## MODIFIED Requirements

### Requirement: Encrypted provider-neutral credential storage

Kestrix SHALL store Git credentials in PostgreSQL as authenticated encrypted data referenced by a stable `credential_ref`. The encryption master key SHALL remain outside PostgreSQL and SHALL be supplied only by the validated file path configured through `KESTRIX_MASTER_KEY_PATH`. Plaintext credential values SHALL NOT be stored in pipeline definitions, run metadata, logs, command arguments, or API responses. Kestrix SHALL NOT use an OS keyring or other implicit fallback to obtain the encryption master key.

#### Scenario: Store a GitHub or GitLab token

- **WHEN** an operator creates a token credential for a GitHub or GitLab host
- **THEN** Kestrix SHALL normalize and encrypt the token before inserting it into PostgreSQL
- **AND** subsequent metadata responses SHALL omit the token value

#### Scenario: Store a Bitbucket app password

- **WHEN** an operator creates a Bitbucket app-password credential
- **THEN** Kestrix SHALL store it using the provider-neutral username/password representation
- **AND** the database row SHALL contain ciphertext rather than either secret value
