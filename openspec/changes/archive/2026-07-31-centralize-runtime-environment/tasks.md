## 1. Environment snapshot and singleton

- [x] 1.1 Define the immutable `internal/config` environment model for Kestrix-owned settings, generic variables, and normalized forwarded variables.
- [x] 1.2 Implement a pure environment loader plus a `sync.Once`-guarded startup singleton; expose test-safe construction without mutating the production singleton.
- [x] 1.3 Add canonical-name-only, initialization-once, and typed-field unit tests.
- [x] 1.4 Replace string-key environment access in application code with typed `Environment` fields; keep raw names only in the loader alias mapping.

## 2. Canonical names and migration surface

- [x] 2.1 Rename Kestrix-owned entries in `.env.template` to `KESTRIX_*`, including server, webhook, dispatch, test, and generated dispatch-context names.
- [x] 2.2 Update Docker Compose files, Makefile commands, scripts, CLI help, README, and relevant docs to advertise `KESTRIX_*` while preserving generic and provider-owned variables.
- [x] 2.3 Remove `METACI_*` compatibility aliases; legacy names must not affect runtime configuration or forwarded variables.

## 3. Migrate application access

- [x] 3.1 Initialize the environment singleton at the executable entry point before subcommand or server setup and apply its values through the existing config-file/CLI precedence flow.
- [x] 3.2 Replace direct environment reads in `cmd/kestrix`, web handlers, dispatch/webhook code, and runtime helpers with singleton/configuration accessors.
- [x] 3.3 Update environment-prefix forwarding and generated dispatch variables to use canonical `KESTRIX_*` names only.
- [x] 3.4 Update application and integration test fixtures to use canonical names and preserve generic database variables.

## 4. Verification and migration safety

- [x] 4.1 Add an automated source check or test proving direct `os.Getenv`, `os.LookupEnv`, and raw Kestrix prefix scans are confined to the configuration boundary and test fixtures.
- [x] 4.2 Verify secrets and connection values do not appear in startup warnings, logs, serialized configuration, or API responses.
- [x] 4.3 Run formatting, vet/static analysis, unit/integration tests, and the project build; verify `.env.template` and Docker configuration use the canonical names.
- [x] 4.4 Run strict OpenSpec validation for `centralize-runtime-environment` and record every requirement scenario as covered.
