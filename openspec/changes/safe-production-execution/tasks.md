## 1. Opt-in guard

- [ ] 1.1 `runner.allow_local` / `KESTRIX_RUNNER_ALLOW_LOCAL` (default `false`) in `internal/config`, following the precedence rule. `RunnerConfig.Validate()` fails for mode `local`/unset without it, naming the key files and both ways out. Unit tests for each combination.
- [ ] 1.2 `validateProject` rejects `mode: local` when the opt-in is off. `RunService` fails a stored `local` project's run before source acquisition with the same message. Startup logs a warning with the count of stored `local` projects. Unit tests; the service coverage gate holds.

## 2. Development and tests keep working

- [ ] 2.1 Set `KESTRIX_RUNNER_ALLOW_LOCAL=true` in `docker/docker-compose.yml`, `make serve`/`make dev`/`make ui-serve`, `.air.toml` if it builds the serve command, `scripts/docker-deployment-e2e.sh`, and every Go e2e/integration harness that starts a server in `local` mode. `scripts/test-all.sh` green.

## 3. Production compose

- [ ] 3.1 `docker/docker-compose.prod.yml`:
  - `KESTRIX_RUNNER_MODE=remote` on `kestrix`;
  - a `runner` service (profile `runner`, same image, `command: ["kestrix", "runner"]`, `KESTRIX_RUNNER_SERVER=http://kestrix:8080`, `KESTRIX_RUNNER_TOKEN=${KESTRIX_RUNNER_TOKEN:-}`, no key volumes, no database variables);
  - a `runners` network joining only `kestrix` and `runner`.

  `docker compose config` passes with and without `--profile runner`.
- [ ] 3.2 Extend `scripts/docker-deployment-e2e.sh`:
  - the default profile starts only `kestrix` and `postgres`;
  - with `--profile runner` and a token registered through the API, a task runs on the runner;
  - `cat` of the key path from a task fails because the file is absent there;
  - the runner cannot reach `postgres:5432`.

## 4. Docs

- [ ] 4.1 `docs/deployment.md`: a "Task execution" section (register a runner in Settings → Runners, `.env`, `--profile runner`) and the opt-in for single-user installs. `SECURITY.md` "Known limitations": `local` mode exposes the key files and is opt-in. `docs/ci-tools.md` runner section. `CHANGELOG.md` `[Unreleased]`: **BREAKING** `Changed` entry.

## 5. Verification

- [ ] 5.1 `scripts/test-all.sh` all layers, `make lint`; 100% unit coverage gate holds.
- [ ] 5.2 Manual, with the production compose on a clean host:
  - the first start succeeds with no runner registered;
  - a project run waits for a runner;
  - after registering one and `--profile runner up -d`, the run completes;
  - a pipeline task running `ls /srv/kestrix` (or the configured key directory) finds nothing.
