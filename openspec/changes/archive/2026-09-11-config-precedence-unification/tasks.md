## 1. One reconciliation path in `internal/config`

- [x] 1.1 Widen the presence shadow struct in `Load` (`internal/config/config.go:224-258`) to every yaml key that has an env or flag form (`listen_addr`, `database_url`, `pipeline.dir`, `webhook.secret`, `webhook.history_limit`, `dispatch.project_id`, `dispatch.gitlab_url`, `dispatch.gitlab_token`, plus the existing `source_cache.*` and `runner.*`), and verify a unit test asserts an explicitly empty key counts as set while an omitted key does not.
- [x] 1.2 Add `func (c *Config) ApplyEnvironment(env *Environment) error` that fills only absent keys from `env` for every setting in 1.1 (including `ListenAddr`, `Pipeline.Dir`, `Dispatch.GitLabURL`, `DatabaseURL`), delete `ApplyWebhookEnvironment`, `ApplySourceCacheEnvironment`, and `ApplyRunnerEnvironment`, and verify a table test covers file-set, file-absent-env-set, and both-absent for each setting.
- [x] 1.3 Replace `ApplyFlags` (`config.go:362`) and `ApplySourceCacheFlags` with one `ApplyFlags(f FlagValues)` taking a struct of parsed flag values and applying only non-zero ones, and verify a unit test asserts a flag beats a file value that beats an env value for one setting of each type (string, int, duration).
- [x] 1.4 Enable `KnownFields(true)` on the yaml decoder in `Load` and verify tests assert an unknown top-level key, an unknown nested key (`runner.executor`), and a `docker:` block each fail with an error naming the key and path and containing no value from the file.

## 2. Thin `cmdServe`, pure `buildServeConfig`

- [x] 2.1 Extract `buildServeConfig(cfg *config.Config, env *config.Environment) web.Config` from `cmd/kestrix/main.go:274-320`, sourcing every file-or-flag setting from `cfg` and only env-only settings from `env`, and verify `go vet` passes and the server still starts with `make dev`.
- [x] 2.2 Rewrite `cmdServe` as parse flags → `Load` → `ApplyEnvironment` → `ApplyFlags` → `SourceCache.Validate` → `buildServeConfig`, deleting the env-to-flag fill at `main.go:200-227` and the inline overrides at `:229-272`, and verify the existing `main_test.go` cases still pass.
- [x] 2.3 Keep `resolveWebhookMethod` and the `-method` flag as an env-or-flag setting (no yaml form), and verify a test asserts flag beats env for it.

## 3. Remove the dead surfaces

- [x] 3.1 Remove `DockerConfig` from `config.Config`, `DockerEnvironment` from `Environment`, `web.DockerConfig` and `web.Config.Docker`, the four `-docker-*` flags, and `RunnerConfig.Executor` / `web.RunnerConfig.Executor`, and verify `go build ./...` and `go vet ./...` pass with no remaining reference (`grep -rn "DockerConfig\|\.Executor\b" --include=*.go cmd internal` empty outside tests).
- [x] 3.2 Update the environment audit test (`internal/config/environment_audit_test.go`) if it lists `DOCKER_*` as typed settings, and verify the `runtime-configuration` "No direct process reads remain" check still passes.

## 4. Exhaustive precedence table

- [x] 4.1 Add a table-driven test in `cmd/kestrix` that, for every multi-form setting, feeds `(config.yaml content, environ, flags)` triples into `Load` + `ApplyEnvironment` + `ApplyFlags` + `buildServeConfig` and asserts the resulting `web.Config` field for: file only, env only, flag only, file+env (file wins), env+flag (flag wins), file+env+flag (flag wins); verify the test fails if any single Apply step is skipped.
- [x] 4.2 Add a reflection check that enumerates exported fields of `config.Environment` and yaml-tagged fields of `config.Config` (recursively) and fails when a field is missing from the 4.1 table or from an explicit env-only / file-only allowlist, and verify it fails when a dummy field is added.
- [x] 4.3 Add a startup log line per multi-form setting naming which source won (`file`, `env`, `flag`, `default`) without the value, and verify a test asserts no secret value appears in the log output.

## 5. Documentation and release notes

- [x] 5.1 Update `.env.template`: keep `KESTRIX_ADDR`, `KESTRIX_DIR`, `KESTRIX_GITLAB_URL` (now effective), move `DOCKER_*` under the passthrough comment as host variables for tasks, and verify `make dev` starts with the documented setup.
- [x] 5.2 Update the README configuration section with the full precedence rule, a `config.yaml` example that includes `database_url` and the `runner:` block, and the env table rows for the three newly effective variables; remove `-docker-*` from any usage text; verify by reading `kestrix serve -h` against the README.
- [x] 5.3 Add a CHANGELOG entry marked BREAKING listing the four precedence flips, the removed `docker.*` and `runner.executor` keys, and the unknown-key startup error.
- [x] 5.4 Remove `DOCKER_*` from the Kestrix service `environment:` in both Compose files only if they are not in the passthrough list; otherwise leave them and verify `scripts/docker-deployment-e2e.sh` still passes the registry variables to the dogfood tasks.

## 6. Verification

- [x] 6.1 Run `make quality` and verify `internal/config` and `cmd/kestrix` stay at their coverage thresholds.
- [ ] 6.2 Run `scripts/docker-deployment-e2e.sh` and verify the deployed instance starts without a `config.yaml` and with the Compose environment unchanged.
- [x] 6.3 Run `npx -y @fission-ai/openspec@latest validate config-precedence-unification --strict` and verify it is valid after `runner-docker-config-wiring` has been archived.
