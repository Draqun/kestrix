## 1. Configuration and startup contract

- [x] 1.1 Add failing configuration and server-startup tests for a missing, empty, and valid `KESTRIX_MASTER_KEY_PATH`.
- [x] 1.2 Model `KESTRIX_MASTER_KEY_PATH` in the immutable runtime environment and remove the YAML `master_key_path` configuration path.
- [x] 1.3 Require and preflight the configured file-backed provider before Kestrix exposes its HTTP listener, with errors that do not reveal key bytes.

## 2. Credential key-provider hardening

- [x] 2.1 Add unit tests for valid raw/hex files and missing, unreadable, permissive, and malformed key-file failures.
- [x] 2.2 Remove the production OS-keyring/`secret-tool` fallback and ensure PostgreSQL credential stores receive only the validated file provider.
- [x] 2.3 Keep existing ciphertext compatibility covered by a credential-store test that decrypts data using the same supplied key.

## 3. Deployment and documentation

- [x] 3.1 Update development and production Compose configuration to mount a separately provisioned read-only key file and pass `KESTRIX_MASTER_KEY_PATH`.
- [x] 3.2 Replace YAML/keyring guidance in remote-project and repository-credential documentation with environment-variable setup, backup, and upgrade instructions.
- [x] 3.3 Add or update deployment coverage for the documented container key mount and startup failure when it is absent.
- [x] 3.4 Use `KESTRIX_MASTER_KEY_PATH` as the only operator-facing host and runtime path; remove the redundant second path convention.
- [x] 3.5 Add the master-key path to `.env.template` and load `.env` for `make dev` and `make dev-all`.

## 4. Verification

- [x] 4.1 Run focused configuration, credentials, web-server, and deployment tests.
- [x] 4.2 Run `make quality`, the integration test suite, and Docker E2E coverage required by the changed deployment path.
- [x] 4.3 Run `openspec validate configure-master-key-path --strict --no-interactive` and verify no master-key material is present in tracked files.
