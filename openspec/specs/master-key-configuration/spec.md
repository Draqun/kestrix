# Master Key Configuration Specification

## Purpose

TBD - Update Purpose after archive.

## Requirements

### Requirement: Explicit master-key path configuration

Kestrix SHALL obtain the credential-encryption master-key file path solely from
`KESTRIX_MASTER_KEY_PATH`. The server SHALL fail before accepting HTTP requests
when the variable is absent or empty. Kestrix SHALL NOT read a
`master_key_path` YAML setting or fall back to an OS keyring, `secret-tool`, or
any generated key.

#### Scenario: Server starts with an explicit key path

- **WHEN** `KESTRIX_MASTER_KEY_PATH` names a valid key file
- **THEN** Kestrix SHALL initialize credential encryption with that file
- **AND** the server SHALL start without invoking an OS keyring

#### Scenario: Master-key path is not configured

- **WHEN** Kestrix starts without a non-empty `KESTRIX_MASTER_KEY_PATH`
- **THEN** startup SHALL fail before the HTTP listener is available
- **AND** the error SHALL identify the missing configuration variable without exposing key material

### Requirement: Master-key file preflight validation

Before the HTTP server starts, Kestrix SHALL verify that the configured path is
readable, is not accessible to group or other users, and contains either 32 raw
bytes or 64 hexadecimal characters representing a 256-bit key. Validation
errors SHALL identify the configuration or file condition but SHALL NOT include
the key contents.

#### Scenario: Invalid configured key file

- **WHEN** `KESTRIX_MASTER_KEY_PATH` points to a missing, permissive, unreadable, or malformed key file
- **THEN** Kestrix SHALL fail startup before serving HTTP
- **AND** no key bytes SHALL be emitted in the error or logs

#### Scenario: Existing valid key is mounted for an upgrade

- **WHEN** an operator provides the same valid key file used to encrypt existing credential rows
- **THEN** Kestrix SHALL retain the existing encryption format
- **AND** previously stored credentials SHALL remain decryptable

### Requirement: Documented deployment key mount

Kestrix deployment documentation and Compose examples SHALL describe mounting a
separately provisioned key file read-only and setting
`KESTRIX_MASTER_KEY_PATH` to its runtime path. The key file SHALL NOT be
committed to the repository or placed in PostgreSQL volumes.

#### Scenario: Operator configures a container deployment

- **WHEN** an operator follows the production deployment instructions
- **THEN** the Kestrix container SHALL receive the master-key path through `KESTRIX_MASTER_KEY_PATH`
- **AND** the documented key-file mount SHALL be distinct from PostgreSQL storage
