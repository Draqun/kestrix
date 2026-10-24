## ADDED Requirements

### Requirement: Master key rotation

Kestrix SHALL provide `kestrix keys rotate-master --new-key-path <file>`,
which connects to the database directly and re-encrypts every stored
ciphertext from the configured master key to the key in `<file>`:
- repository and runtime-secret credentials;
- remote job secrets;
- remote run secrets.

The new key file SHALL pass the same validation as the configured key and
SHALL differ from it. Every row SHALL be re-encrypted with the associated
data it already uses. The rotation SHALL be one transaction: every rewritten
value SHALL be decrypted with the new key before commit, and any failure
SHALL leave every row encrypted under the configured key. On success the
command SHALL report the number of values re-encrypted per kind and
instruct the operator to replace the configured key file with the new one
before starting Kestrix.

#### Scenario: Rotation re-encrypts everything
- **WHEN** an operator runs the rotation with a valid new key while no server is running
- **THEN** every stored credential and remote secret SHALL decrypt with the new key
- **AND** none SHALL decrypt with the old key

#### Scenario: Failed rotation changes nothing
- **WHEN** the rotation fails after some rows were rewritten in its transaction
- **THEN** every stored value SHALL still decrypt with the configured key

#### Scenario: Same key rejected
- **WHEN** `--new-key-path` names a key identical to the configured one
- **THEN** the command SHALL fail without changing any row

### Requirement: Rotation refuses while a server is running

A running Kestrix server SHALL hold a database-wide advisory lock for its
whole lifetime. `kestrix keys rotate-master` SHALL acquire that lock before
changing any row, and SHALL fail without changing anything, stating that a
Kestrix server is connected, when it cannot.

#### Scenario: Server still running
- **WHEN** an operator runs the rotation while `kestrix serve` is connected to the same database
- **THEN** the command SHALL fail stating that a server is connected
- **AND** no row SHALL change

### Requirement: Startup detects a mismatched master key

After the master-key file passes validation, and when at least one
credential is stored, Kestrix SHALL decrypt one stored credential before
serving requests. If decryption fails, startup SHALL fail with a message
stating that the configured key does not decrypt stored credentials and
naming `kestrix keys rotate-master`. With no stored credential, startup SHALL
proceed.

#### Scenario: Key file swapped without rotation
- **WHEN** the master-key file is replaced by a different valid key and Kestrix starts with credentials stored
- **THEN** startup SHALL fail with the mismatch message before serving any request
