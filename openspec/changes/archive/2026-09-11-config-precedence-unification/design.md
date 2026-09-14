## Context

See proposal.md — Why. What shapes the approach:

- `config.Load` (`internal/config/config.go:210-259`) already records key
  presence, but only for `source_cache.*` and `runner.*`, via a second
  unmarshal into a pointer-typed shadow struct. That is the mechanism rule A
  needs; it is just not applied to the other keys.
- `cmdServe` (`cmd/kestrix/main.go:141-337`) does the reconciliation inline:
  env fills empty flags (`:200-227`), flags overwrite `cfg` (`:229-272`), then
  a 45-line literal builds `web.Config` (`:274-320`) mixing `cfg.*` and `env.*`
  by hand. Nothing is unit-testable without starting the server.
- Three `Apply*Environment` functions exist with two different rules
  (`ApplyWebhookEnvironment` env-wins, the other two file-wins), and
  `Config.ApplyFlags` (`config.go:362`) has no caller.
- `Environment` bakes defaults into its fields (`RunnerDockerPIDs = 256`,
  `HTTPReadTimeout = 30s`, …), so "env unset" and "env set to the default" are
  indistinguishable there. Rule A tolerates that: the file's presence decides,
  and when the file is silent the env value or its default is what should
  apply anyway.
- `scripts/test.sh` holds `internal/config` and `cmd/kestrix` under the 100%
  unit gate, so every removed branch is also removed coverage risk.

```
 today                                    after

 config.yaml ─┐                          config.yaml ──► Load (presence for every key)
 KESTRIX_*  ──┼─► env fills flags ─┐           │
 flags ──────┘   flags overwrite ──┤           ▼
                 3× Apply*Env ─────┤      ApplyEnvironment(env)   fill absent keys only
                 45-line literal ──┘           │
                       │                       ▼
                       ▼                  ApplyFlags(flags)       non-empty flags win
                  web.Config                   │
                                               ▼
                                     buildServeConfig(cfg, env) ─► web.Config
                                     (pure; env used only for env-only settings)
```

## Goals / Non-Goals

**Goals:** one rule, one code path, one test that proves both for every
setting. No form Kestrix accepts is silently ignored.

**Non-Goals:** new forms; the CLI subcommands that never load a file; the
`gitlab-runner` mode (see proposal); changing what `Environment` validates.

## Decisions

### D1. Presence for every key, one `ApplyEnvironment`, one `ApplyFlags`

The shadow-struct presence detection in `Load` is widened to every yaml key
that has an environment or flag form. `ApplyEnvironment(env)` replaces the
three `Apply*Environment` functions with one loop of `if !set && envValue !=
zero { c.X = envValue }` statements, and `ApplyFlags(f FlagValues)` replaces
both the dead `ApplyFlags` and the inline blocks in `cmdServe`, taking a
struct of parsed flag values so the signature stops growing per setting.

Alternative: a generic reflection-driven mapper (struct tags `env:"..."`,
`flag:"..."`). Rejected — it hides the precedence in a library nobody reads at
3am, and the explicit version is about forty lines.

Alternative: keep the env-to-flag fill and only reorder. Rejected — that
pattern is the bug: it makes the environment look like a CLI flag, which is
why env beats file today.

### D2. Wire the four dead forms; delete the two dead surfaces

| Form | Decision | Why |
|---|---|---|
| `KESTRIX_ADDR`, `KESTRIX_DIR`, `KESTRIX_GITLAB_URL` | wire | one line each in `ApplyEnvironment`; documented in `.env.template:15,18,135`; natural for containers |
| yaml `database_url` | wire | the key exists and the README config example invites it; a file-set URL beats `DATABASE_URL` per rule A |
| `docker.*` (yaml, `-docker-*`, `Environment.Docker`, `web.DockerConfig`) | delete | dead end-to-end since runtime-secrets D3 removed the only reader (`internal/web/handler_helpers.go:20-28` comment); `DOCKER_*` stay as passthrough host variables for dogfood scripts (`docker/docker-compose.yml:65`) |
| `runner.executor` | delete | no reader; the only conceivable consumer, `gitlab-runner exec` (`internal/runner/runner.go:298`), does not exist in gitlab-runner 19.3.1 |

Deleting rather than wiring the dead surfaces is the smaller diff and removes
two tables of tests that would assert values going nowhere.

### D3. Precedence flip is breaking and stays breaking

For `webhook.secret`, `webhook.history_limit`, `dispatch.project_id`, and
`dispatch.gitlab_token` the file now beats the environment. An operator who
committed a placeholder to `config.yaml` and relied on the environment to
override it must delete the key from the file. Compose deployments ship no
`config.yaml`, so the common case sees no change. A compatibility mode was
considered and rejected: two rules is the defect.

### D4. `buildServeConfig` is the test seam

`cmdServe` is split into a thin shell (parse flags, `Load`,
`ApplyEnvironment`, `ApplyFlags`, `Validate`) and a pure
`buildServeConfig(cfg *config.Config, env *config.Environment) web.Config`.
Every value that has a file or flag form comes from `cfg`; only env-only
settings (key paths, throttle, CSP, cookie, HTTP bounds) come from `env`. One
table-driven test in `cmd/kestrix` feeds `(file yaml, environ, flags)` triples
and asserts the resulting `web.Config` field. A reflection check walks
`config.Environment` and the yaml-tagged fields of `config.Config` and fails
if a field is missing from the table, so the class cannot return unnoticed.

### D5. Unknown config-file keys are errors

`Load` uses `yaml.Decoder.KnownFields(true)`. A removed key (`docker:`,
`runner.executor`) or a typo fails startup with the key name and file path,
never the value. This is the cheapest guard against "modelled, never
consumed" recurring in the file form; the reflection check in D4 guards the
environment form.

### D6. Ordering against `runner-docker-config-wiring`

That change adds `WithRunnerConfig` and `ReplaceCurrent` and keeps
`ApplyRunnerEnvironment`. This change lands after it and folds
`ApplyRunnerEnvironment` into `ApplyEnvironment` without changing its rule;
the derived-snapshot swap stays as designed there. The MODIFIED precedence
requirement in this delta therefore already carries that change's two
scenarios.

## Security considerations

- Secrets move up in precedence: a `webhook.secret` or `dispatch.gitlab_token`
  in a mode-0600 `config.yaml` now wins over the environment. The file was
  already an accepted place for those values (`Save` writes 0600); the change
  is which copy wins, not where secrets may live.
- The unknown-key error names the key and path, never the value, so a
  misplaced token in the file is not echoed to logs.
- Removing `-docker-password` removes a way for a registry password to land
  in shell history and `ps` output; the dogfood scripts already read it from
  the environment through the allowlist.

## Risks / Trade-offs

- [Operators with both forms set for a flipped setting] → CHANGELOG marks it
  BREAKING; startup logs which source won for each setting, values omitted.
- [`KnownFields` rejects files that were previously accepted] → the error
  names the key; the two removed keys are listed in the CHANGELOG with their
  replacement (none needed for `docker.*`; `runner.executor` had no effect).
- [Reflection-based completeness test becomes a maintenance nag] → that is its
  job; adding a setting without its table row is exactly what it must block.

## Migration Plan

1. Land `runner-docker-config-wiring` first.
2. Deploy; operators with a `config.yaml` remove `docker:` and
   `runner.executor` if present, and any placeholder keys they expected the
   environment to override.
3. Rollback is a plain revert; no data changes.
