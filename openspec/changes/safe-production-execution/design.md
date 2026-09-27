## Context

- The instance `runner.mode` defaults to `local` and is validated by
  `RunnerConfig.Validate()` in `cmdServe`. Each project stores its own mode,
  backfilled once from the instance mode (`internal/store/pgstore.go`).
- `local` runs `bash -c` in the Kestrix process's container, as its user.
- The production compose file mounts `KESTRIX_MASTER_KEY_PATH` and
  `KESTRIX_JWT_SIGNING_KEY_PATH` read-only into that container. It sets no
  mode, mounts no Docker socket, and the image has no `USER` directive.
- `kestrix runner` is the same binary. It needs only
  `KESTRIX_RUNNER_SERVER` and `KESTRIX_RUNNER_TOKEN`, runs tasks with its own
  engine (`local` in the image), and never sees Kestrix's keys or database.

```
today (prod compose)                     after
┌──────────── kestrix ───────────┐       ┌──────────── kestrix ─────────┐   net: runners
│ master.key  jwt.key  (root)    │       │ master.key  jwt.key          │◀──────────────┐
│ bash -c <pipeline script> ─────┼─ can  │ mode: remote, no local exec  │               │
└────────────────────────────────┘  read └──────────────────────────────┘      ┌─ runner ─────────┐
                                                                                │ kestrix runner   │
                                                                                │ no keys, no DB   │
                                                                                │ bash -c <script> │
                                                                                └──────────────────┘
```

## Decisions

### D1. An explicit opt-in, checked in three places

`runner.allow_local` (config key) / `KESTRIX_RUNNER_ALLOW_LOCAL` (env),
default `false`, same precedence as every other setting. Without it:

- `RunnerConfig.Validate()` fails when the instance mode is `local` or unset.
  The message names the key files and says: use `remote` with a runner, or
  set the opt-in on a single-user machine.
- `validateProject` rejects `mode: local`. An empty mode resolving to a
  non-local instance mode is fine.
- `RunService` fails a run of a stored `local` project before acquiring
  sources, with the same message. At startup a warning names how many stored
  projects are `local`.

Rationale: fail closed, like the webhook secret. The default is the safe
state, and the unsafe one needs a written decision. Changing only the
default mode would leave `mode: local` one API call away.

`docker` mode in-process is untouched. It needs the Docker socket, which is
already a deliberate operator choice.

### D2. The production compose runs tasks on a keyless runner

- `kestrix` gets `KESTRIX_RUNNER_MODE=remote`.
- A `runner` service is added behind profile `runner`, so "Only Kestrix and
  its database start by default" still holds. It uses the same image, runs
  `command: ["kestrix", "runner"]`, and gets `KESTRIX_RUNNER_SERVER=http://kestrix:8080`
  and `KESTRIX_RUNNER_TOKEN`. It has no key volumes and no database variables.
- A `runners` network joins `kestrix` and `runner` only, so the runner cannot
  reach `postgres`.
- The token is read with `${KESTRIX_RUNNER_TOKEN:-}`, not `:?`. Compose
  interpolates every service eagerly, so a required-guard would break the
  default `up`. `kestrix runner` already refuses to start without a token.

The operator flow goes into `docs/deployment.md`: first admin, then
Settings → Runners → register, then token into `.env`, then
`--profile runner up -d`.

### D3. Development stays one command

`docker/docker-compose.yml` (which mounts the Docker socket for `docker`
mode), `make serve`/`make dev`, `scripts/docker-deployment-e2e.sh` and the
Go e2e/integration harnesses set the opt-in explicitly. That names the risk
where it is accepted.

## Security Considerations

- After this change no default configuration runs a pipeline script in a
  process that can read the master or signing key.
- The runner still runs as root inside its own container, with the job's
  resolved secrets and its runner token. That is inherent to executing the
  job and bounded to that container. It cannot reach the database.
- The opt-in is per instance, not per project. An instance that enables it
  accepts the exposure for every project, and the startup warning says so.

## Risks / Trade-offs

- A first production start now needs a runner before any task runs. That is
  one documented extra step, in exchange for not handing out the keys.
- Existing development databases with `local` projects keep working only
  with the opt-in set. Intended.

## Migration Plan

No schema change. Rollback: the previous image ignores the new setting and
runs `local` again.
