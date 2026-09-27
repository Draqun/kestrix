## 1. Server-side guards

- [ ] 1.1 `kestrix serve` holds `pg_advisory_lock(<constant>)` on a dedicated connection for its lifetime (design D2). Integration test: a second `pg_try_advisory_lock` on the same constant fails while the server runs and succeeds after it stops.
- [ ] 1.2 Startup key check (D3): with at least one credential, a key that cannot decrypt it stops startup with the documented message; with none, startup proceeds. Integration tests for match, mismatch and empty.

## 2. Rotation CLI

- [ ] 2.1 `kestrix keys rotate-master --new-key-path <file>` following `kestrix user create` (DB-direct, `DATABASE_URL`/`--database-url`). It validates both keys (mode `0600`, format, keys differ) and refuses while the advisory lock is held. Unit tests for each refusal.
- [ ] 2.2 The one-transaction sweep over `credentials.encrypted_payload`, `remote_jobs.secrets_enc` and `runs.remote_secrets_enc`, with each column's existing AAD, using `credentials.Rewrap`, verifying every rewritten row with the new key, and printing counts per column. Integration tests:
  - all three columns re-encrypted and readable with the new key only;
  - an injected failure mid-sweep leaves every row readable with the old key.

## 3. Docs

- [ ] 3.1 `docs/deployment.md`: "Rotating keys" (master: stop, run the CLI through `docker compose run --rm` with the new key mounted, swap, start; JWT: replace and restart, all sessions end) and a "Suspected key leak" checklist. `docs/authentication.md` and `SECURITY.md` link to it. `CHANGELOG.md` `[Unreleased]`: `Added` entry.

## 4. Verification

- [ ] 4.1 `scripts/test-all.sh` all layers, `make lint`; 100% unit coverage gate holds.
- [ ] 4.2 Manual, on local Docker Compose with one repository credential and one runtime secret:
  - rotate the master key per the docs; a run that uses both still passes;
  - starting with the old key file afterwards is refused with the D3 message;
  - rotating the JWT key signs the UI out.
