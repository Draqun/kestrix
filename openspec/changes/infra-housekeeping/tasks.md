## 1. Compose and Docker tooling

- [x] 1.1 Remove the `redis` service block and the `redis_data` volume from `docker/docker-compose.yml` (lines 80-99) and `docker/docker-compose.prod.yml` (lines 80-90), and verify `docker compose -f docker/docker-compose.yml config` and `docker compose -f docker/docker-compose.prod.yml config` both succeed with `KESTRIX_MASTER_KEY_PATH` and `KESTRIX_JWT_SIGNING_KEY_PATH` set to any readable file and list no `redis` service.
- [x] 1.2 Remove Redis from `docker/Makefile`: the `Redis: localhost:6380` banner line (29), `redis` from `deps-up` (36) and `deps-up-d` (40), and the two `Redis` lines in the `status` target (105-106); update the top-level `Makefile` `docker-up` comment (233) to "PostgreSQL", and verify `make -C docker help`, `make help`, and `make docker-up` run without referencing Redis.
- [x] 1.3 Remove `REDIS_PORT` and its comment mention from `.env.template` (133-136), and verify `grep -n -i redis .env.template` returns nothing and `make dev` still loads `.env` without error.

## 2. Docker E2E script

- [x] 2.1 Remove the `REDIS_PORT` derivation (line 11) and export (line 99) from `scripts/docker-deployment-e2e.sh`, and verify `bash -n scripts/docker-deployment-e2e.sh` passes and `grep -n -i redis scripts/` returns nothing.
- [x] 2.2 Run `make docker-e2e` on a host with Docker and verify it passes end to end, so the isolated stack starts, registers, and cleans up without the service.

## 3. Documentation

- [x] 3.1 Replace `docs/terraform-provider.md` with a pointer document: purpose (manage Kestrix project registration as infrastructure-as-code), repository `https://gitlab.com/kestrix/terraform-provider-kestrix`, authentication (same bearer token and roles as `docs/authentication.md`), and a statement that resource schema, examples, and release status live in that repository; remove the resource table, example HCL, and "early development" claim, and verify every remaining link resolves within the repository.
- [x] 3.2 Update `docs/overview.md` row 19 so the Terraform provider description says it is maintained in a separate repository, and verify `make ui-build` copies the updated docs into `ui/dist/docs`.
- [x] 3.3 Remove the Redis mentions from `AGENTS.md` line 23 (`postgres + redis`), `docs/usage.md` line 98 (`PostgreSQL/Redis/cache volumes`), and `docker/README.md` line 3 (the identity list), and verify `grep -rn -i redis AGENTS.md README.md docs/ docker/README.md` returns nothing.
- [x] 3.4 Add a CHANGELOG `Unreleased → Removed` entry naming the dropped Redis service and `redis_data` volume, the removed `REDIS_PORT` variable, the `docker volume rm` step for existing hosts, and the provider doc change; leave the `v0.2.0` lines 52-53 unchanged, and verify the entry reads correctly in the rendered file.

## 4. Verification

- [x] 4.1 Run `grep -rn -i redis --exclude-dir=node_modules --exclude-dir=.git --exclude-dir=dist .` and verify the only remaining hits are `CHANGELOG.md` (the historical `v0.2.0` lines and the new Unreleased entry), the vendored OpenSpec skill text under `.claude/`, `.opencode/`, `.agents/`, and `openspec/changes/archive/`.
- [x] 4.2 Verify `grep -rn -i "business" docker/ .env.template ui/business docs/business-website` shows every business-site file, service, and variable byte-identical to `main`, confirming the non-goal held.
- [x] 4.3 Run `make quality` and verify it passes; nothing in Go changes, so this confirms no test referenced the Compose Redis port.
- [x] 4.4 Run `npx -y @fission-ai/openspec@latest validate infra-housekeeping --strict` and verify it passes.
