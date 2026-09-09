#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${KESTRIX_E2E_COMPOSE_FILE:-${REPO_ROOT}/docker/docker-compose.prod.yml}"
PROJECT_NAME="${KESTRIX_E2E_PROJECT:-kestrix-e2e-$$}"
PROJECT_NAME="${PROJECT_NAME//[^a-zA-Z0-9_-]/-}"
BASE_PORT="${KESTRIX_E2E_PORT:-$((28080 + ($$ % 1000)))}"
BUSINESS_PORT="${KESTRIX_E2E_BUSINESS_PORT:-$((BASE_PORT + 2))}"
POSTGRES_PORT="${KESTRIX_E2E_POSTGRES_PORT:-$((BASE_PORT + 4))}"
HEALTH_TIMEOUT="${KESTRIX_E2E_TIMEOUT:-120}"
HEALTH_URL="http://127.0.0.1:${BASE_PORT}/api/health"
IMAGE_OVERRIDE="${KESTRIX_E2E_IMAGE:-}"
IMAGE_TAG="${KESTRIX_E2E_IMAGE_TAG:-kestrix:e2e-${PROJECT_NAME}}"
TEMP_DIR=""
OVERRIDE_FILE=""
IMAGE_BUILT=0

if docker compose version >/dev/null 2>&1; then
  COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE=(docker-compose)
else
  echo "Docker Compose is required (docker compose or docker-compose)." >&2
  exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "Docker is required but was not found in PATH." >&2
  exit 1
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required but was not found in PATH." >&2
  exit 1
fi
if ! command -v git >/dev/null 2>&1; then
  echo "git is required but was not found in PATH." >&2
  exit 1
fi
if [[ ! -f "${COMPOSE_FILE}" ]]; then
  echo "Production Compose file not found: ${COMPOSE_FILE}" >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon is unavailable. Start Docker and retry." >&2
  exit 1
fi

TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/kestrix-docker-e2e.XXXXXX")"
OVERRIDE_FILE="${TEMP_DIR}/docker-compose.override.yml"
REMOTE_BARE="${TEMP_DIR}/remote.git"
REMOTE_WORK="${TEMP_DIR}/remote-work"
SHARED_BARE="${TEMP_DIR}/shared.git"
SHARED_WORK="${TEMP_DIR}/shared-work"
MASTER_KEY_FILE="${TEMP_DIR}/master.key"
JWT_SIGNING_KEY_FILE="${TEMP_DIR}/jwt.key"
ADMIN_NAME="e2e-admin"
ADMIN_PASSWORD="e2e-admin-password-$$"

mkdir -p "${REMOTE_WORK}/ci"
git init --quiet --bare --initial-branch=main "${REMOTE_BARE}"
git -C "${REMOTE_WORK}" init --quiet --initial-branch=main
git -C "${REMOTE_WORK}" remote add origin "${REMOTE_BARE}"
printf '%s\n' \
  'project "docker-remote" {' \
  '  ci_repo "app" { path = "." }' \
  '  ci_repo "shared" {' \
  '    url = "file:///e2e/shared.git"' \
  '    ref = "main"' \
  '  }' \
  '  ci_task "verify" {' \
  '    trigger = "repo.app.changed"' \
  '    execute {' \
  '      repo = "app"' \
  '      script = "test -f version.txt"' \
  '    }' \
  '  }' \
  '  ci_task "print_env" {' \
  '    trigger = "repo.app.changed"' \
  '    execute {' \
  '      repo = "app"' \
  '      script = "env | sort"' \
  '    }' \
  '  }' \
  '  ci_task "sleep_task" {' \
  '    trigger = "repo.app.changed"' \
  '    execute {' \
  '      repo = "app"' \
  '      script = "sleep 120"' \
  '    }' \
  '  }' \
  '  ci_task "sync_shared" {' \
  '    trigger = "repo.shared.changed"' \
  '    execute {' \
  '      repo = "shared"' \
  '      script = "test -f lib/util.go"' \
  '    }' \
  '  }' \
  '}' > "${REMOTE_WORK}/ci/pipeline.tf"
printf '%s\n' 'docker-e2e-source' > "${REMOTE_WORK}/version.txt"
git -C "${REMOTE_WORK}" add .
git -C "${REMOTE_WORK}" -c user.name='Kestrix Docker E2E' -c user.email=e2e@example.test commit --quiet -m initial
git -C "${REMOTE_WORK}" push --quiet -u origin main
REMOTE_COMMIT_SHA="$(git -C "${REMOTE_WORK}" rev-parse HEAD)"

# A second bare remote declared by the pipeline as ci_repo "shared": a push to
# it must reach the project through its derived sources (multi-source-projects).
mkdir -p "${SHARED_WORK}/lib"
git init --quiet --bare --initial-branch=main "${SHARED_BARE}"
git -C "${SHARED_WORK}" init --quiet --initial-branch=main
git -C "${SHARED_WORK}" remote add origin "${SHARED_BARE}"
printf '%s\n' 'package util' > "${SHARED_WORK}/lib/util.go"
git -C "${SHARED_WORK}" add .
git -C "${SHARED_WORK}" -c user.name='Kestrix Docker E2E' -c user.email=e2e@example.test commit --quiet -m initial
git -C "${SHARED_WORK}" push --quiet -u origin main

dd if=/dev/urandom of="${MASTER_KEY_FILE}" bs=32 count=1 status=none
chmod 600 "${MASTER_KEY_FILE}"
dd if=/dev/urandom of="${JWT_SIGNING_KEY_FILE}" bs=32 count=1 status=none
chmod 600 "${JWT_SIGNING_KEY_FILE}"
# Task-execution-isolation checks below need `docker` runner mode so a task
# actually starts a container to inspect. That needs the `docker` CLI and
# the host's socket inside the deployment container (Docker-outside-of-
# Docker) - test-only, never part of the shipped image or Compose file. The
# bind-mounted host /tmp makes DockerExecutor's per-run workdir (created
# under the container's /tmp) resolve to the same path when the *host*
# daemon services the `-v` mount for a sibling task container.
STARTUP_SCRIPT='apk add --no-cache docker-cli bash >/dev/null'
STARTUP_SCRIPT="${STARTUP_SCRIPT} && mkdir -p /root/.kestrix"
STARTUP_SCRIPT="${STARTUP_SCRIPT} && printf 'runner:\n  mode: docker\n' > /root/.kestrix/config.yaml"
STARTUP_SCRIPT="${STARTUP_SCRIPT} && git config --global --add safe.directory /e2e/remote.git"
STARTUP_SCRIPT="${STARTUP_SCRIPT} && git config --global --add safe.directory /e2e/shared.git"
STARTUP_SCRIPT="${STARTUP_SCRIPT} && exec /kestrix serve --addr :8080 --static ./ui/dist"
printf '%s\n' \
  'services:' \
  '  kestrix:' \
  '    entrypoint: ["/bin/sh", "-c"]' \
  "    command: [\"${STARTUP_SCRIPT}\"]" \
  '    volumes:' \
  '      - source_cache:/var/lib/kestrix/cache/git' \
  "      - ${REMOTE_BARE}:/e2e/remote.git:ro" \
  "      - ${SHARED_BARE}:/e2e/shared.git:ro" \
  '      - /var/run/docker.sock:/var/run/docker.sock' \
  '      - /tmp:/tmp' > "${OVERRIDE_FILE}"

export KESTRIX_IMAGE="${IMAGE_OVERRIDE:-${IMAGE_TAG}}"
export KESTRIX_MASTER_KEY_PATH="${MASTER_KEY_FILE}"
export KESTRIX_JWT_SIGNING_KEY_PATH="${JWT_SIGNING_KEY_FILE}"
export KESTRIX_PORT="${BASE_PORT}"
export BUSINESS_PORT
export BUSINESS_BIND_HOST=127.0.0.1
export POSTGRES_PORT

compose() {
  "${COMPOSE[@]}" -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" -f "${OVERRIDE_FILE}" "$@"
}

if env -u KESTRIX_MASTER_KEY_PATH "${COMPOSE[@]}" -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" config >/dev/null 2>&1; then
  echo "Production Compose unexpectedly accepts a missing KESTRIX_MASTER_KEY_PATH." >&2
  exit 1
fi

diagnose() {
  echo "--- Docker Compose status (${PROJECT_NAME}) ---" >&2
  compose ps >&2 || true
  echo "--- Docker Compose logs (${PROJECT_NAME}) ---" >&2
  compose logs --no-color --tail=200 >&2 || true
}

cleanup() {
  local exit_code=$?
  set +e
  if [[ "${exit_code}" -ne 0 ]]; then
    diagnose
  fi
  compose down --volumes --remove-orphans >/dev/null 2>&1
  if [[ "${IMAGE_BUILT}" -eq 1 ]]; then
    docker image rm "${IMAGE_TAG}" >/dev/null 2>&1
  fi
  [[ -z "${TEMP_DIR}" ]] || rm -rf "${TEMP_DIR}"
  exit "${exit_code}"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

echo "=== Docker deployment E2E ==="
echo "Compose project: ${PROJECT_NAME}"
echo "Image:           ${KESTRIX_IMAGE}"
echo "API:             ${HEALTH_URL}"

if [[ -z "${IMAGE_OVERRIDE}" ]]; then
  echo "Building deployment image..."
  docker build -f "${REPO_ROOT}/docker/Dockerfile" -t "${IMAGE_TAG}" "${REPO_ROOT}"
  IMAGE_BUILT=1
else
  echo "Using prebuilt image override."
  if ! docker image inspect "${IMAGE_OVERRIDE}" >/dev/null 2>&1; then
    echo "Prebuilt image not found locally: ${IMAGE_OVERRIDE}" >&2
    exit 1
  fi
fi

compose config >/dev/null
compose up -d --force-recreate

wait_for_health() {
  echo "Waiting up to ${HEALTH_TIMEOUT}s for ${HEALTH_URL}..."
  local deadline=$((SECONDS + HEALTH_TIMEOUT))
  local health_body=""
  until health_body="$(curl -fsS --max-time 3 "${HEALTH_URL}" 2>/dev/null)" \
    && [[ "${health_body}" == *'"status"'* ]] \
    && [[ "${health_body}" == *'"ok"'* ]]; do
    if (( SECONDS >= deadline )); then
      echo "Deployment did not become healthy within ${HEALTH_TIMEOUT}s." >&2
      exit 1
    fi
    sleep 2
  done
  echo "Health response: ${health_body}"
}

wait_for_health

# Kestrix serves no /api route until an administrator exists - see
# user-authentication spec, "Server never runs unauthenticated". Bootstrap
# one exactly the way an operator would: the CLI, run inside the container
# against its own DATABASE_URL, reading the password from stdin so it never
# appears in the process table.
echo "Bootstrapping the first administrator account..."
printf '%s\n' "${ADMIN_PASSWORD}" | compose exec -T kestrix \
  /kestrix user create --role admin "${ADMIN_NAME}"

LOGIN_RESPONSE="${TEMP_DIR}/login.json"
status="$(curl -sS -o "${LOGIN_RESPONSE}" -w '%{http_code}' --max-time 10 \
  -X POST "http://127.0.0.1:${BASE_PORT}/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"${ADMIN_NAME}\",\"password\":\"${ADMIN_PASSWORD}\"}")"
if [[ "${status}" != "200" ]]; then
  echo "Login as the bootstrapped administrator returned HTTP ${status}:" >&2
  sed -n '1,120p' "${LOGIN_RESPONSE}" >&2
  exit 1
fi
API_TOKEN="$(sed -n 's/.*"token":"\([^"]*\)".*/\1/p' "${LOGIN_RESPONSE}")"
if [[ -z "${API_TOKEN}" ]]; then
  echo "Login response did not contain a token." >&2
  exit 1
fi
AUTH_HEADER=(-H "Authorization: Bearer ${API_TOKEN}")
echo "Authenticated as ${ADMIN_NAME}."

# Cookie sessions: login also sets kestrix_session, and a cookie-authenticated
# mutation must carry X-Kestrix-Request or is rejected with 403 - see
# session-cookie-auth design.md D3. The bearer path above must stay
# unaffected; this only exercises the cookie path a browser would use.
echo "Verifying the cookie session and its CSRF header requirement..."
COOKIE_JAR="${TEMP_DIR}/cookies.txt"
status="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 10 -c "${COOKIE_JAR}" \
  -X POST "http://127.0.0.1:${BASE_PORT}/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"${ADMIN_NAME}\",\"password\":\"${ADMIN_PASSWORD}\"}")"
if [[ "${status}" != "200" ]]; then
  echo "Cookie-session login returned HTTP ${status}." >&2
  exit 1
fi

RESOLVE_BODY='{"changed":[],"dir":""}'
BEARER_RESOLVE_STATUS="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 10 \
  "${AUTH_HEADER[@]}" -H 'Content-Type: application/json' \
  -X POST "http://127.0.0.1:${BASE_PORT}/api/resolve" -d "${RESOLVE_BODY}")"

status="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 10 -b "${COOKIE_JAR}" \
  -H 'Content-Type: application/json' \
  -X POST "http://127.0.0.1:${BASE_PORT}/api/resolve" -d "${RESOLVE_BODY}")"
if [[ "${status}" != "403" ]]; then
  echo "Cookie POST without X-Kestrix-Request returned HTTP ${status}, want 403." >&2
  exit 1
fi

status="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 10 -b "${COOKIE_JAR}" \
  -H 'Content-Type: application/json' -H 'X-Kestrix-Request: 1' \
  -X POST "http://127.0.0.1:${BASE_PORT}/api/resolve" -d "${RESOLVE_BODY}")"
if [[ "${status}" != "${BEARER_RESOLVE_STATUS}" ]]; then
  echo "Cookie POST with X-Kestrix-Request returned HTTP ${status}, want ${BEARER_RESOLVE_STATUS} (the bearer path's own status)." >&2
  exit 1
fi
echo "Cookie CSRF rule verified: 403 without the header, ${status} with it (matches the bearer path)."

# Session revocation: a deleted account's token must stop working, and must
# do so on the very next request - see design.md D2/D4 (rbac-hardening).
# Deletion happens through this same running process, so no wait for the
# identity-cache TTL is needed.
echo "Verifying a deleted account's token stops working..."
REVOKE_NAME="e2e-revoke-check"
REVOKE_PASSWORD="throwaway-${PROJECT_NAME}"
printf '%s\n' "${REVOKE_PASSWORD}" | compose exec -T kestrix \
  /kestrix user create --role viewer "${REVOKE_NAME}"

REVOKE_LOGIN_RESPONSE="${TEMP_DIR}/revoke-login.json"
status="$(curl -sS -o "${REVOKE_LOGIN_RESPONSE}" -w '%{http_code}' --max-time 10 \
  -X POST "http://127.0.0.1:${BASE_PORT}/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"${REVOKE_NAME}\",\"password\":\"${REVOKE_PASSWORD}\"}")"
if [[ "${status}" != "200" ]]; then
  echo "Login as the throwaway revocation-check account returned HTTP ${status}:" >&2
  sed -n '1,120p' "${REVOKE_LOGIN_RESPONSE}" >&2
  exit 1
fi
REVOKE_TOKEN="$(sed -n 's/.*"token":"\([^"]*\)".*/\1/p' "${REVOKE_LOGIN_RESPONSE}")"
if [[ -z "${REVOKE_TOKEN}" ]]; then
  echo "Revocation-check login response did not contain a token." >&2
  exit 1
fi

status="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 10 \
  -H "Authorization: Bearer ${REVOKE_TOKEN}" "http://127.0.0.1:${BASE_PORT}/api/projects")"
if [[ "${status}" != "200" ]]; then
  echo "Throwaway account's token was rejected before deletion (HTTP ${status})." >&2
  exit 1
fi

REVOKE_DELETE_RESPONSE="${TEMP_DIR}/revoke-delete-response"
status="$(curl -sS -o "${REVOKE_DELETE_RESPONSE}" -w '%{http_code}' --max-time 10 \
  "${AUTH_HEADER[@]}" -X DELETE "http://127.0.0.1:${BASE_PORT}/api/users/${REVOKE_NAME}")"
if [[ "${status}" != "204" ]]; then
  echo "Deleting the throwaway account returned HTTP ${status}:" >&2
  sed -n '1,120p' "${REVOKE_DELETE_RESPONSE}" >&2
  exit 1
fi

status="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 10 \
  -H "Authorization: Bearer ${REVOKE_TOKEN}" "http://127.0.0.1:${BASE_PORT}/api/projects")"
if [[ "${status}" != "401" ]]; then
  echo "Deleted account's token still worked (HTTP ${status}), want 401." >&2
  exit 1
fi
echo "Deleted account's token correctly rejected with 401."

assert_json_endpoint() {
  local path="$1"
  local expected_field="$2"
  local response_file="${TEMP_DIR}/response"
  local status
  status="$(curl -sS -o "${response_file}" -w '%{http_code}' --max-time 10 "${AUTH_HEADER[@]}" "http://127.0.0.1:${BASE_PORT}${path}")"
  if [[ "${status}" != "200" ]]; then
    echo "${path} returned HTTP ${status}; response:" >&2
    sed -n '1,120p' "${response_file}" >&2
    exit 1
  fi
  if [[ "${expected_field}" == "array" ]]; then
    if ! grep -q '^\[' "${response_file}"; then
      echo "${path} response is not a JSON array:" >&2
      sed -n '1,120p' "${response_file}" >&2
      exit 1
    fi
  elif ! grep -q "\"${expected_field}\"" "${response_file}"; then
    echo "${path} response is missing JSON field '${expected_field}':" >&2
    sed -n '1,120p' "${response_file}" >&2
    exit 1
  fi
  echo "${path}: HTTP ${status} (${expected_field} response verified)"
}

assert_json_endpoint /api/projects items
assert_json_endpoint /api/runs array

API_BASE="http://127.0.0.1:${BASE_PORT}"
SETTINGS_HTML="${TEMP_DIR}/settings.html"
SETTINGS_BUNDLE="${TEMP_DIR}/settings.js"
status="$(curl -sS -o "${SETTINGS_HTML}" -w '%{http_code}' --max-time 10 "${API_BASE}/settings")"
if [[ "${status}" != "200" ]] || ! grep -q '<div id="root"></div>' "${SETTINGS_HTML}"; then
  echo "Direct /settings route did not return the production SPA (HTTP ${status})." >&2
  exit 1
fi
SETTINGS_ASSET="$(sed -n 's/.*src="\([^"]*\.js\)".*/\1/p' "${SETTINGS_HTML}" | head -n 1)"
if [[ -z "${SETTINGS_ASSET}" ]]; then
  echo "Production SPA did not reference a JavaScript bundle." >&2
  exit 1
fi
curl -fsS --max-time 20 "${API_BASE}${SETTINGS_ASSET}" > "${SETTINGS_BUNDLE}"
if ! grep -q '/settings' "${SETTINGS_BUNDLE}" || ! grep -q 'Repository credentials' "${SETTINGS_BUNDLE}"; then
  echo "Production bundle does not contain the Settings credential section." >&2
  exit 1
fi
echo "/settings: HTTP ${status} (SPA route and credential section verified)"

SECRET_VALUE='e2e-secret-do-not-log'
CREDENTIAL_RESPONSE="${TEMP_DIR}/credential.json"
status="$(curl -sS -o "${CREDENTIAL_RESPONSE}" -w '%{http_code}' --max-time 10 "${AUTH_HEADER[@]}" \
  -X POST "${API_BASE}/api/credentials" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"Docker E2E\",\"type\":\"token\",\"provider\":\"git\",\"host\":\"local\",\"token\":\"${SECRET_VALUE}\"}")"
if [[ "${status}" != "201" ]]; then
  echo "Credential creation returned HTTP ${status}:" >&2
  sed -n '1,120p' "${CREDENTIAL_RESPONSE}" >&2
  exit 1
fi
if grep -q "${SECRET_VALUE}" "${CREDENTIAL_RESPONSE}"; then
  echo "Credential response exposed the secret value." >&2
  exit 1
fi
CREDENTIAL_REF="$(sed -n 's/.*"ref":"\([^"]*\)".*/\1/p' "${CREDENTIAL_RESPONSE}")"
if [[ -z "${CREDENTIAL_REF}" ]]; then
  echo "Credential response did not contain a reference." >&2
  exit 1
fi

WEBHOOK_SECRET_VALUE="e2e-webhook-secret-$$"
GITLAB_PROJECT_ID=424242

# Registration goes straight into PostgreSQL rather than through
# POST /api/projects: repo_url validation (webhook-ingress-hardening
# design.md D6) accepts only https://, ssh://, and scp-like remotes, and
# this project's file:// bare-repo mount exists to exercise the deployment
# against a real Git remote without standing up a network transport, not
# the create-time URL check (covered by internal/service's own tests).
compose exec -T postgres psql -U kestrix -d kestrix -v ON_ERROR_STOP=1 -c \
  "INSERT INTO projects (name, repo_url, repo_ref, repo_credential_ref, pipeline_dir, pipeline_file, gitlab_project_id, webhook_secret) VALUES ('docker-remote', 'file:///e2e/remote.git', 'main', '${CREDENTIAL_REF}', 'ci', 'pipeline.tf', ${GITLAB_PROJECT_ID}, '${WEBHOOK_SECRET_VALUE}');" \
  >/dev/null

PROJECT_RESPONSE="${TEMP_DIR}/project.json"
status="$(curl -sS -o "${PROJECT_RESPONSE}" -w '%{http_code}' --max-time 20 "${AUTH_HEADER[@]}" "${API_BASE}/api/projects/docker-remote")"
if [[ "${status}" != "200" ]]; then
  echo "Reading the seeded remote project returned HTTP ${status}:" >&2
  sed -n '1,120p' "${PROJECT_RESPONSE}" >&2
  exit 1
fi
if grep -Eq "${SECRET_VALUE}|${WEBHOOK_SECRET_VALUE}|\"webhook_secret\"|repo_path|source-session|/var/lib/kestrix/cache" "${PROJECT_RESPONSE}"; then
  echo "Project API response exposed secret or host/cache path data." >&2
  exit 1
fi
if ! grep -q '"has_webhook_secret":true' "${PROJECT_RESPONSE}"; then
  echo "Project response did not report has_webhook_secret:true:" >&2
  sed -n '1,120p' "${PROJECT_RESPONSE}" >&2
  exit 1
fi

# webhook-ingress-hardening: verification fails closed. No global
# KESTRIX_WEBHOOK_SECRET is configured for this deployment, so a delivery
# for the registered project is authenticated by its own per-project secret
# alone.
echo "Verifying webhook delivery fails closed without a secret..."
WEBHOOK_PAYLOAD="{\"object_kind\":\"push\",\"ref\":\"refs/heads/main\",\"checkout_sha\":\"${REMOTE_COMMIT_SHA}\",\"project\":{\"id\":${GITLAB_PROJECT_ID},\"path_with_namespace\":\"docker-remote\"},\"commits\":[{\"id\":\"${REMOTE_COMMIT_SHA}\",\"modified\":[\"version.txt\"]}]}"
UNAUTHENTICATED_WEBHOOK_RESPONSE="${TEMP_DIR}/webhook-unauthenticated.json"
status="$(curl -sS -o "${UNAUTHENTICATED_WEBHOOK_RESPONSE}" -w '%{http_code}' --max-time 20 \
  -X POST "${API_BASE}/webhook" \
  -H 'Content-Type: application/json' \
  -d "${WEBHOOK_PAYLOAD}")"
if [[ "${status}" != "401" ]]; then
  echo "Unauthenticated webhook delivery returned HTTP ${status}, want 401:" >&2
  sed -n '1,120p' "${UNAUTHENTICATED_WEBHOOK_RESPONSE}" >&2
  exit 1
fi
echo "Unauthenticated webhook delivery correctly rejected with 401."

echo "Verifying webhook delivery succeeds with the project's own secret..."
AUTHENTICATED_WEBHOOK_RESPONSE="${TEMP_DIR}/webhook-authenticated.json"
status="$(curl -sS -o "${AUTHENTICATED_WEBHOOK_RESPONSE}" -w '%{http_code}' --max-time 30 \
  -X POST "${API_BASE}/webhook" \
  -H 'Content-Type: application/json' \
  -H "X-Gitlab-Token: ${WEBHOOK_SECRET_VALUE}" \
  -d "${WEBHOOK_PAYLOAD}")"
if [[ "${status}" != "200" ]]; then
  echo "Webhook delivery with the project's secret returned HTTP ${status}, want 200:" >&2
  sed -n '1,200p' "${AUTHENTICATED_WEBHOOK_RESPONSE}" >&2
  exit 1
fi
echo "Webhook delivery with the project's own secret accepted with 200."

# multi-source-projects: loading the definition derives the project's sources
# (the row above was seeded straight into PostgreSQL, so nothing derived them
# yet): the primary first, then the shared remote declared by the pipeline.
curl -fsS --max-time 30 "${AUTH_HEADER[@]}" "${API_BASE}/api/projects/docker-remote/definition" > "${TEMP_DIR}/definition.json"
curl -fsS --max-time 10 "${AUTH_HEADER[@]}" "${API_BASE}/api/projects/docker-remote" > "${TEMP_DIR}/project-sources.json"
if ! grep -q '"name":"shared"' "${TEMP_DIR}/project-sources.json" \
  || ! grep -q '"url":"file:///e2e/shared.git"' "${TEMP_DIR}/project-sources.json" \
  || ! grep -q '"primary":true' "${TEMP_DIR}/project-sources.json"; then
  echo "Project sources were not derived from the pipeline:" >&2
  sed -n '1,40p' "${TEMP_DIR}/project-sources.json" >&2
  exit 1
fi
echo "/api/projects/docker-remote: derived sources verified"

# Push to the shared remote and deliver its webhook. No global secret is
# configured, so the delivery is accepted only because it presents the
# matched project's own secret; it must resolve docker-remote through the
# secondary source and trigger only sync_shared.
echo "Verifying a push to the shared remote resolves the project through its secondary source..."
printf '%s\n' 'package util // v2' > "${SHARED_WORK}/lib/util.go"
git -C "${SHARED_WORK}" add .
git -C "${SHARED_WORK}" -c user.name='Kestrix Docker E2E' -c user.email=e2e@example.test commit --quiet -m 'shared v2'
git -C "${SHARED_WORK}" push --quiet origin main
SHARED_SHA="$(git -C "${SHARED_WORK}" rev-parse HEAD)"
SHARED_PAYLOAD="{\"object_kind\":\"push\",\"ref\":\"refs/heads/main\",\"checkout_sha\":\"${SHARED_SHA}\",\"project\":{\"id\":0,\"path_with_namespace\":\"e2e/shared\",\"git_http_url\":\"file:///e2e/shared.git\"},\"commits\":[{\"id\":\"${SHARED_SHA}\",\"added\":[],\"modified\":[\"lib/util.go\"],\"removed\":[]}]}"
status="$(curl -sS -o "${TEMP_DIR}/shared-unauthenticated.json" -w '%{http_code}' --max-time 20 \
  -X POST "${API_BASE}/webhook" -H 'Content-Type: application/json' -d "${SHARED_PAYLOAD}")"
if [[ "${status}" != "401" ]]; then
  echo "Unauthenticated secondary-source webhook returned HTTP ${status}, want 401." >&2
  exit 1
fi
SHARED_WEBHOOK_RESPONSE="${TEMP_DIR}/shared-webhook.json"
status="$(curl -sS -o "${SHARED_WEBHOOK_RESPONSE}" -w '%{http_code}' --max-time 30 \
  -X POST "${API_BASE}/webhook" \
  -H 'Content-Type: application/json' \
  -H "X-Gitlab-Token: ${WEBHOOK_SECRET_VALUE}" \
  -d "${SHARED_PAYLOAD}")"
if [[ "${status}" != "200" ]]; then
  echo "Secondary-source webhook returned HTTP ${status}:" >&2
  sed -n '1,40p' "${SHARED_WEBHOOK_RESPONSE}" >&2
  exit 1
fi
if ! grep -q '"source_name":"shared"' "${SHARED_WEBHOOK_RESPONSE}" || ! grep -q '"project":"docker-remote"' "${SHARED_WEBHOOK_RESPONSE}" || ! grep -q '"tasks":\["sync_shared"\]' "${SHARED_WEBHOOK_RESPONSE}"; then
  echo "Secondary-source webhook did not resolve the project through its shared source:" >&2
  sed -n '1,40p' "${SHARED_WEBHOOK_RESPONSE}" >&2
  exit 1
fi
curl -fsS --max-time 10 "${AUTH_HEADER[@]}" "${API_BASE}/api/logs?project=docker-remote" > "${TEMP_DIR}/logs.json"
if ! grep -q '"source_name":"shared"' "${TEMP_DIR}/logs.json"; then
  echo "Webhook history does not attribute the delivery to the shared source:" >&2
  sed -n '1,40p' "${TEMP_DIR}/logs.json" >&2
  exit 1
fi
echo "/webhook: secondary source resolved docker-remote (source_name=shared)"

# task-execution-isolation: a task process must never see a control-plane
# secret, even running as a container in the deployment image.
# KESTRIX_TEST_DATABASE_URL is the one KESTRIX_* name expected here: it is
# on this Compose file's own KESTRIX_RUNNER_ENV_PASSTHROUGH default, and the
# design accepts it (the test helper refuses any database not ending in
# _test). print_env's own output is the task's complete view of its
# environment - see design.md D1/D2.
echo "Verifying a task process cannot see control-plane secrets..."
ENV_TASK_RESPONSE="${TEMP_DIR}/env-task.json"
status="$(curl -sS -o "${ENV_TASK_RESPONSE}" -w '%{http_code}' --max-time 60 "${AUTH_HEADER[@]}" \
  -X POST "${API_BASE}/api/projects/docker-remote/tasks/print_env/run" \
  -H 'Content-Type: application/json' -d '{}')"
if [[ "${status}" != "200" ]]; then
  echo "print_env task run returned HTTP ${status}:" >&2
  sed -n '1,200p' "${ENV_TASK_RESPONSE}" >&2
  exit 1
fi
if grep -Eq '(^|\\n)DATABASE_URL=|KESTRIX_MASTER_KEY_PATH=|KESTRIX_JWT_SIGNING_KEY_PATH=|KESTRIX_GITLAB_TOKEN=|KESTRIX_WEBHOOK_SECRET=' "${ENV_TASK_RESPONSE}"; then
  echo "Task process leaked a control-plane secret variable:" >&2
  sed -n '1,200p' "${ENV_TASK_RESPONSE}" >&2
  exit 1
fi
if ! grep -q 'PATH=' "${ENV_TASK_RESPONSE}"; then
  echo "Task process is missing the base allowlist (PATH):" >&2
  sed -n '1,200p' "${ENV_TASK_RESPONSE}" >&2
  exit 1
fi
if ! grep -q 'KESTRIX_TEST_DATABASE_URL=' "${ENV_TASK_RESPONSE}"; then
  echo "Task process is missing the expected KESTRIX_RUNNER_ENV_PASSTHROUGH entry (KESTRIX_TEST_DATABASE_URL):" >&2
  sed -n '1,200p' "${ENV_TASK_RESPONSE}" >&2
  exit 1
fi
echo "Task environment allowlist verified: no control-plane secret leaked."

# runtime-secrets: a `secret` credential resolved for a pipeline variable
# must reach the task as an environment value, and its value must never
# appear in run output, task result output, or the API - only `***` in its
# place. See runtime-secrets design.md D5.
echo "Verifying a runtime secret is injected and redacted from task output..."
RUNTIME_SECRET_VALUE="e2e-runtime-secret-do-not-log-$$"
RUNTIME_SECRET_RESPONSE="${TEMP_DIR}/runtime-secret-credential.json"
status="$(curl -sS -o "${RUNTIME_SECRET_RESPONSE}" -w '%{http_code}' --max-time 10 "${AUTH_HEADER[@]}" \
  -X POST "${API_BASE}/api/credentials" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"Docker E2E runtime secret\",\"type\":\"secret\",\"value\":\"${RUNTIME_SECRET_VALUE}\"}")"
if [[ "${status}" != "201" ]]; then
  echo "Runtime secret credential creation returned HTTP ${status}:" >&2
  sed -n '1,120p' "${RUNTIME_SECRET_RESPONSE}" >&2
  exit 1
fi
if grep -q "${RUNTIME_SECRET_VALUE}" "${RUNTIME_SECRET_RESPONSE}"; then
  echo "Runtime secret credential response exposed the value." >&2
  exit 1
fi
RUNTIME_SECRET_REF="$(sed -n 's/.*"ref":"\([^"]*\)".*/\1/p' "${RUNTIME_SECRET_RESPONSE}")"
if [[ -z "${RUNTIME_SECRET_REF}" ]]; then
  echo "Runtime secret credential response did not contain a reference." >&2
  exit 1
fi

# Amend the registered project's pipeline with a task that echoes the
# secret variable, and push it - handleRunTask reads the current HEAD of
# the project's configured ref, so this new commit is what the task run
# below executes.
printf '%s\n' \
  'project "docker-remote" {' \
  '  ci_repo "app" { path = "." }' \
  '  ci_task "verify" {' \
  '    trigger = "repo.app.changed"' \
  '    execute {' \
  '      repo = "app"' \
  '      script = "test -f version.txt"' \
  '    }' \
  '  }' \
  '  ci_task "print_env" {' \
  '    trigger = "repo.app.changed"' \
  '    execute {' \
  '      repo = "app"' \
  '      script = "env | sort"' \
  '    }' \
  '  }' \
  '  ci_task "sleep_task" {' \
  '    trigger = "repo.app.changed"' \
  '    execute {' \
  '      repo = "app"' \
  '      script = "sleep 120"' \
  '    }' \
  '  }' \
  '  ci_task "echo_secret" {' \
  '    trigger = "repo.app.changed"' \
  '    execute {' \
  '      repo = "app"' \
  '      script = "echo DEPLOY_SECRET=$DEPLOY_SECRET"' \
  '    }' \
  '  }' \
  '  variable "DEPLOY_SECRET" {' \
  "    secret_ref = \"${RUNTIME_SECRET_REF}\"" \
  '    scope      = "project"' \
  '  }' \
  '}' > "${REMOTE_WORK}/ci/pipeline.tf"
git -C "${REMOTE_WORK}" add ci/pipeline.tf
git -C "${REMOTE_WORK}" -c user.name='Kestrix Docker E2E' -c user.email=e2e@example.test commit --quiet -m 'add runtime secret task'
git -C "${REMOTE_WORK}" push --quiet origin main

ECHO_SECRET_RESPONSE="${TEMP_DIR}/echo-secret.json"
status="$(curl -sS -o "${ECHO_SECRET_RESPONSE}" -w '%{http_code}' --max-time 30 "${AUTH_HEADER[@]}" \
  -X POST "${API_BASE}/api/projects/docker-remote/tasks/echo_secret/run" \
  -H 'Content-Type: application/json' -d '{}')"
if [[ "${status}" != "200" ]]; then
  echo "echo_secret task run returned HTTP ${status}:" >&2
  sed -n '1,200p' "${ECHO_SECRET_RESPONSE}" >&2
  exit 1
fi
if grep -q "${RUNTIME_SECRET_VALUE}" "${ECHO_SECRET_RESPONSE}"; then
  echo "Task run response (run output via API) leaked the runtime secret value:" >&2
  sed -n '1,200p' "${ECHO_SECRET_RESPONSE}" >&2
  exit 1
fi
if ! grep -q '\*\*\*' "${ECHO_SECRET_RESPONSE}"; then
  echo "Task run response did not contain the redaction marker:" >&2
  sed -n '1,200p' "${ECHO_SECRET_RESPONSE}" >&2
  exit 1
fi

RUN_LIST_RESPONSE="${TEMP_DIR}/echo-secret-run-list.json"
curl -fsS --max-time 10 "${AUTH_HEADER[@]}" "${API_BASE}/api/projects/docker-remote/runs?limit=10" > "${RUN_LIST_RESPONSE}"
# Runs are ordered newest-first and "id" is domain.Run's first JSON field, so
# it is also the first field of the first (newest) array element - anchor on
# that exact literal rather than a bare `.*"id":`, which a greedy match would
# otherwise satisfy at the *last* "id" in the array (the oldest run).
ECHO_SECRET_RUN_ID="$(sed -n 's/.*"items":\[{"id":\([0-9]*\).*/\1/p' "${RUN_LIST_RESPONSE}")"
if [[ -z "${ECHO_SECRET_RUN_ID}" ]]; then
  echo "Could not determine the echo_secret run's id:" >&2
  sed -n '1,200p' "${RUN_LIST_RESPONSE}" >&2
  exit 1
fi
ECHO_SECRET_RUN_RESPONSE="${TEMP_DIR}/echo-secret-run.json"
curl -fsS --max-time 10 "${AUTH_HEADER[@]}" "${API_BASE}/api/projects/docker-remote/runs/${ECHO_SECRET_RUN_ID}" > "${ECHO_SECRET_RUN_RESPONSE}"
if grep -q "${RUNTIME_SECRET_VALUE}" "${ECHO_SECRET_RUN_RESPONSE}"; then
  echo "Persisted run record (task result output) leaked the runtime secret value:" >&2
  sed -n '1,200p' "${ECHO_SECRET_RUN_RESPONSE}" >&2
  exit 1
fi
if ! grep -q '\*\*\*' "${ECHO_SECRET_RUN_RESPONSE}"; then
  echo "Persisted run record did not contain the redaction marker:" >&2
  sed -n '1,200p' "${ECHO_SECRET_RUN_RESPONSE}" >&2
  exit 1
fi
echo "Runtime secret verified: injected into the task, redacted everywhere it is returned."

# task-execution-isolation: cancelling a run must stop its docker-mode
# task's container - see the "Cancel a running Docker task" scenario.
echo "Verifying cancellation stops a docker-mode task's container..."
SLEEP_RUN_RESPONSE="${TEMP_DIR}/sleep-run.json"
status="$(curl -sS -o "${SLEEP_RUN_RESPONSE}" -w '%{http_code}' --max-time 20 "${AUTH_HEADER[@]}" \
  -X POST "${API_BASE}/api/projects/docker-remote/run" \
  -H 'Content-Type: application/json' -d '{}')"
if [[ "${status}" != "200" ]]; then
  echo "Starting the run for sleep_task returned HTTP ${status}:" >&2
  sed -n '1,200p' "${SLEEP_RUN_RESPONSE}" >&2
  exit 1
fi
SLEEP_RUN_ID="$(sed -n 's/.*"run_id":\([0-9]*\).*/\1/p' "${SLEEP_RUN_RESPONSE}")"
if [[ -z "${SLEEP_RUN_ID}" ]]; then
  echo "Run response did not contain a run_id:" >&2
  sed -n '1,200p' "${SLEEP_RUN_RESPONSE}" >&2
  exit 1
fi

RUN_STATE="${TEMP_DIR}/sleep-run-state.json"
deadline=$((SECONDS + 60))
until curl -sS --max-time 10 "${AUTH_HEADER[@]}" "${API_BASE}/api/projects/docker-remote/runs/${SLEEP_RUN_ID}" -o "${RUN_STATE}" \
  && grep -q '"name":"sleep_task","status":"running"' "${RUN_STATE}"; do
  if (( SECONDS >= deadline )); then
    echo "sleep_task did not reach status running within timeout:" >&2
    sed -n '1,200p' "${RUN_STATE}" >&2
    exit 1
  fi
  sleep 1
done

CONTAINER_NAME="$(docker ps --filter 'name=kestrix-sleep_task-' --format '{{.Names}}' | head -n1)"
if [[ -z "${CONTAINER_NAME}" ]]; then
  echo "Expected a running kestrix-sleep_task-* container; found none:" >&2
  docker ps >&2
  exit 1
fi
echo "sleep_task container running: ${CONTAINER_NAME}"

status="$(curl -sS -o "${TEMP_DIR}/sleep-cancel-response.json" -w '%{http_code}' --max-time 30 "${AUTH_HEADER[@]}" \
  -X POST "${API_BASE}/api/projects/docker-remote/runs/${SLEEP_RUN_ID}/cancel")"
if [[ "${status}" != "200" ]]; then
  echo "Cancelling the sleep_task run returned HTTP ${status}:" >&2
  sed -n '1,200p' "${TEMP_DIR}/sleep-cancel-response.json" >&2
  exit 1
fi

deadline=$((SECONDS + 20))
while docker ps --filter "name=${CONTAINER_NAME}" --format '{{.Names}}' | grep -q "${CONTAINER_NAME}"; do
  if (( SECONDS >= deadline )); then
    echo "Container ${CONTAINER_NAME} is still running after cancellation:" >&2
    docker ps >&2
    exit 1
  fi
  sleep 1
done
echo "Container ${CONTAINER_NAME} is absent from 'docker ps' after cancellation."

PIPELINE_BEFORE="${TEMP_DIR}/pipeline-before.tf"
status="$(curl -sS -o "${PIPELINE_BEFORE}" -w '%{http_code}' --max-time 20 "${AUTH_HEADER[@]}" "${API_BASE}/api/projects/docker-remote/pipeline")"
if [[ "${status}" != "200" ]] || ! grep -q 'docker-remote' "${PIPELINE_BEFORE}"; then
  echo "Remote pipeline read failed before restart (HTTP ${status})." >&2
  exit 1
fi
CACHE_BEFORE="$(compose exec -T kestrix sh -c 'find /var/lib/kestrix/cache/git -type d -name mirror.git | sort')"
if [[ -z "${CACHE_BEFORE}" ]]; then
  echo "Source cache was not populated." >&2
  exit 1
fi

compose restart kestrix >/dev/null
wait_for_health
CACHE_AFTER="$(compose exec -T kestrix sh -c 'find /var/lib/kestrix/cache/git -type d -name mirror.git | sort')"
if [[ "${CACHE_AFTER}" != "${CACHE_BEFORE}" ]]; then
  echo "Source cache did not persist unchanged across container restart." >&2
  exit 1
fi
status="$(curl -sS -o "${TEMP_DIR}/pipeline-after.tf" -w '%{http_code}' --max-time 20 "${AUTH_HEADER[@]}" "${API_BASE}/api/projects/docker-remote/pipeline")"
if [[ "${status}" != "200" ]]; then
  echo "Cached pipeline read failed after restart (HTTP ${status})." >&2
  exit 1
fi

status="$(curl -sS -o "${TEMP_DIR}/revoke-response" -w '%{http_code}' --max-time 10 "${AUTH_HEADER[@]}" \
  -X POST "${API_BASE}/api/credentials/${CREDENTIAL_REF}/revoke")"
if [[ "${status}" != "204" ]]; then
  echo "Credential revocation returned HTTP ${status}." >&2
  exit 1
fi
status="$(curl -sS -o "${TEMP_DIR}/revoked-read.json" -w '%{http_code}' --max-time 20 "${AUTH_HEADER[@]}" "${API_BASE}/api/projects/docker-remote/pipeline")"
if [[ "${status}" != "400" ]] || ! grep -qi 'revoked' "${TEMP_DIR}/revoked-read.json"; then
  echo "Cached source did not fail closed after credential revocation (HTTP ${status})." >&2
  exit 1
fi

curl -fsS --max-time 10 "${AUTH_HEADER[@]}" "${API_BASE}/api/credentials" > "${TEMP_DIR}/credentials.json"
curl -fsS --max-time 10 "${AUTH_HEADER[@]}" "${API_BASE}/api/projects/docker-remote" > "${TEMP_DIR}/project-read.json"
compose logs --no-color > "${TEMP_DIR}/compose.log"
if grep -R -q "${SECRET_VALUE}" "${CREDENTIAL_RESPONSE}" "${PROJECT_RESPONSE}" "${TEMP_DIR}/credentials.json" "${TEMP_DIR}/project-read.json" "${TEMP_DIR}/revoked-read.json" "${TEMP_DIR}/compose.log"; then
  echo "Credential secret was found in an API response or deployment log." >&2
  exit 1
fi
if grep -q "${WEBHOOK_SECRET_VALUE}" "${TEMP_DIR}/project-read.json" || grep -q "\"webhook_secret\"" "${TEMP_DIR}/project-read.json"; then
  echo "Webhook secret was found in the project read response." >&2
  exit 1
fi
if grep -R -q "${WEBHOOK_SECRET_VALUE}" "${TEMP_DIR}/compose.log"; then
  echo "Webhook secret was found in the deployment log." >&2
  exit 1
fi
if grep -R -q "${ADMIN_PASSWORD}" "${TEMP_DIR}/compose.log"; then
  echo "Administrator password was found in the deployment log." >&2
  exit 1
fi
if grep -R -q "${RUNTIME_SECRET_VALUE}" "${RUNTIME_SECRET_RESPONSE}" "${ECHO_SECRET_RESPONSE}" "${ECHO_SECRET_RUN_RESPONSE}" "${TEMP_DIR}/compose.log"; then
  echo "Runtime secret value was found in an API response or the deployment/container log." >&2
  exit 1
fi

echo "=== Docker deployment E2E passed ==="
