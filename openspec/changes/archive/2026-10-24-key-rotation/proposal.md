## Why

Neither key Kestrix depends on can be rotated, so a leaked key cannot be
recovered from. `safe-production-execution` closes the main way to leak
one; this closes the aftermath.

- **Master key.** It wraps every credential and secret: repository and
  runtime `secret` credentials (`credentials.encrypted_payload`), and remote
  job and run secrets (`remote_jobs.secrets_enc`, `runs.remote_secrets_enc`).
  Replacing the key file does not fail at startup. Every stored row fails to
  decrypt later, when first read. `credentials.Rewrap` exists but nothing
  calls it, and `FileMasterKeyProvider` ignores the stored `key_version`.
  The only recovery from a leaked key is re-creating every credential by
  hand.
- **JWT signing key.** Replacing the file invalidates every session.
  `docs/authentication.md` says so, but there is no procedure, and nothing
  tells an operator that this is also what to do after a leak.

## What Changes

- New CLI `kestrix keys rotate-master --new-key-path <file>`, following
  `kestrix user create`. It connects to the database directly and re-encrypts
  every ciphertext column from the current key (`KESTRIX_MASTER_KEY_PATH`)
  to the new one in one transaction. Before committing, it decrypts each
  rewritten row with the new key. It prints the file swap as the next step.
- The CLI refuses to run while a Kestrix server is connected to the same
  database. The server holds a Postgres advisory lock for its lifetime, and
  the CLI must acquire it. A server still using the old key could otherwise
  write rows the new key cannot read.
- Startup checks the master key against stored data. If a credential exists
  and the configured key cannot decrypt it, Kestrix refuses to start, naming
  the mismatch and the rotation command. It no longer starts and fails on
  first use.
- The documented procedure for the JWT key: replace the file and restart.
  Every session ends, runner tokens and the metrics token are unaffected.
  `docs/deployment.md` gets both procedures and a "suspected key leak"
  checklist.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `master-key-configuration`: rotation CLI, the running-server guard, and
  the startup key check.
- `user-authentication`: the signing-key rotation procedure is normative.

## Non-goals

- Online rotation with two master keys loaded at once. Offline rotation
  needs no ciphertext-format change and takes seconds.
- JWT `kid` headers or a grace period for old tokens. Sessions last an hour,
  and ending them is the point after a leak.
- Automatic periodic rotation, or external key management (KMS, Vault).
- Fixing the brief `Purpose` of the `master-key-configuration` spec (a known
  `--strict` warning), which is outside this change's deltas.

## Impact

- Downtime: the server must be stopped while the CLI runs; seconds for
  typical data.
- Behaviour: a server whose key does not match stored credentials now
  refuses to start.
- No schema change. `key_version` stays as it is.
- Code: `cmd/kestrix` (new `keys` command), `internal/credentials`,
  `internal/store` (re-encrypt sweep, advisory lock, key check),
  `internal/web/server.go` startup, docs `docs/deployment.md`,
  `docs/authentication.md`, `SECURITY.md`.
