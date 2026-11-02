# Tasks: initial-production-deployment

## 1. Release Cut & Artifact Verification

- [ ] 1.1 Run `scripts/release.sh 0.5.0` on clean `main` branch to tag `v0.5.0`, update changelog, and push commit/tag to origin; verify with `git describe --tags` that the tag exists and working tree is clean.
- [ ] 1.2 Monitor the GitLab tag pipeline until completion via `scripts/wait-green.sh`; verify the `publish-image` and `create-release` jobs succeed.
- [ ] 1.3 Smoke test published release image and binary from outside the runner: verify `docker pull registry.gitlab.com/kestrix/kestrix:v0.5.0` succeeds and `docker run --rm registry.gitlab.com/kestrix/kestrix:v0.5.0 version` prints `v0.5.0`.

## 2. Target Host Provisioning & Stack Startup

- [ ] 2.1 Prepare production deployment environment on target host: provision `.env` with non-default `POSTGRES_PASSWORD`, `DATABASE_URL`, generate 32-byte secret files for `KESTRIX_MASTER_KEY_PATH` and `KESTRIX_JWT_SIGNING_KEY_PATH` with `0600` permissions; verify files exist and are not group/world readable.
- [ ] 2.2 Launch core control plane stack using `docker compose -f docker/docker-compose.prod.yml up -d`; verify `docker compose -f docker/docker-compose.prod.yml ps` shows `kestrix` and `postgres` running healthy without host port exposure for PostgreSQL.
- [ ] 2.3 Verify health endpoint: execute `curl -sf http://localhost:18080/api/health` and assert it returns HTTP 200 with JSON status `ok`.

## 3. Administrative Bootstrap & Security Lockdown

- [ ] 3.1 Navigate to `/api/setup` in the browser or via API to bootstrap the primary administrator account; verify the admin account is successfully created in PostgreSQL.
- [ ] 3.2 Verify setup lockdown: execute a subsequent `GET /api/setup` request and assert that further setup attempts are rejected with HTTP 403 or redirect to login.

## 4. Runner Registration & End-to-End Pipeline Verification

- [ ] 4.1 Authenticate as administrator, navigate to Settings → Runners (or use `/api/runners/token`), and generate a runner registration token; verify token is issued.
- [ ] 4.2 Start the runner service profile on the target host via `docker compose -f docker/docker-compose.prod.yml --profile runner up -d`; verify the runner container starts and reports online in `GET /api/runners`.
- [ ] 4.3 Trigger a test pipeline run: verify the task is claimed by the production runner, executes in isolation (with no key files mounted and no direct DB access), streams terminal output cleanly, and finalizes with status `passed` in the React Flow DAG UI.

## Workflow follow-up

- Archive the change via `openspec archive initial-production-deployment -y` after live deployment verification is signed off.
- Sync final delta specs to `openspec/specs/`.
