## Context

- The master key is a 256-bit key read from a `0600` file
  (`internal/credentials/key.go`). The server builds
  `FileMasterKeyProvider{Path, Version: 1}` and validates it before listening
  (`internal/web/server.go`).
- Encryption is envelope AES-GCM (`internal/credentials/crypto.go`): a
  per-record DEK is wrapped by the master key. The payload is sealed with
  caller AAD:
  - credential metadata for `credentials.encrypted_payload`;
  - `remote_job:<id>` for `remote_jobs.secrets_enc`;
  - `run:<id>` for `runs.remote_secrets_enc`.
- `Rewrap(old, new, aad, blob)` exists, unused.
- The JWT key is HS256, single key, 1 h tokens, no `kid`
  (`internal/auth/token.go`). Runner tokens (SHA-256 hashes) and the metrics
  token do not depend on either key.

```
operator                           kestrix keys rotate-master
  stop kestrix ───────────────▶  pg_try_advisory_lock(K) ── held? → refuse
                                  BEGIN
                                  for each ciphertext row (3 columns):
                                    Rewrap(old, new, aad(row), blob) → UPDATE
                                    Decrypt(new, aad(row), rewritten) (verify)
                                  COMMIT
  swap key file, start ◀──────── "replace KESTRIX_MASTER_KEY_PATH with <new>, then start"
kestrix serve: hold advisory lock K for process lifetime
               startup: decrypt one credential row with the configured key → mismatch? refuse
```

## Decisions

### D1. Offline re-encryption, one transaction

The CLI reads the current key from `KESTRIX_MASTER_KEY_PATH` and the new key
from `--new-key-path`, both validated like today (mode `0600`, 32 bytes raw
or 64 hex). The two keys must differ. In one transaction it selects every
non-null ciphertext from the three columns with `FOR UPDATE`, rewraps each
with the AAD that column already uses, updates it, and decrypts the new
value with the new key before moving on. A failure anywhere rolls back, so
the database is left entirely under the old key. The CLI prints the counts
per column.

Rejected: dual-key online rotation. The envelope has a `key_version`, but the
provider ignores it, and the server hard-codes version 1. Making it
meaningful needs a version-to-key configuration, a lazy or background
re-encrypt, and a "done" signal. The offline sweep reaches the same end
state with none of that, at the cost of a short stop.

### D2. The server holds an advisory lock; the CLI must win it

`kestrix serve` takes `pg_advisory_lock(<constant>)` on a dedicated
connection at startup and keeps it until exit. The CLI uses
`pg_try_advisory_lock` on the same constant and refuses when it cannot get
it: `a Kestrix server is connected to this database; stop it first`.
Postgres releases the lock when a crashed process's connection closes.

### D3. Startup detects a wrong master key

After the key file validates, the server reads one credential row, if any
exists, and decrypts it. A decryption failure stops startup with
`the configured master key does not decrypt stored credentials; restore the
previous key or complete a rotation with 'kestrix keys rotate-master'`.
With no credentials there is nothing to check. Remote secrets are transient
and not used for the check.

### D4. The JWT key rotates by replacement

The procedure: write a new key file, point `KESTRIX_JWT_SIGNING_KEY_PATH` at
it (or replace it in place), restart. Every token signed by the old key
fails verification, so every session ends and users sign in again. That is
already the behaviour. This change makes it the documented, specified
procedure, including for a suspected leak. No CLI is needed.

## Security Considerations

- Plaintexts exist only in the CLI's memory during the sweep, never on disk
  or in logs. Errors name the row's table and ID, never content.
- The new key file must be as protected as the old. The CLI refuses a key
  that is not `0600`, the same as the server.
- The leak checklist in `docs/deployment.md`: rotate the master key, rotate
  the signing key, revoke and re-register runners, rotate the metrics token,
  then rotate the underlying repository tokens at their providers. Those are
  what an attacker holding the master key could already have decrypted.

## Risks / Trade-offs

- A deployment with many credentials holds row locks for the sweep's
  duration. The server is stopped anyway (D2).
- An operator who swaps the key file without running the CLI now gets a
  refused start (D3) instead of a running server that fails on use. That is
  louder, and intended.

## Migration Plan

No schema change. Rollback: the previous image neither holds the advisory
lock nor checks the key; data is unaffected.
