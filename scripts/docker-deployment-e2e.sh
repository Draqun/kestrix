#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${KESTRIX_E2E_COMPOSE_FILE:-${REPO_ROOT}/docker/docker-compose.prod.yml}"
PROJECT_NAME="${KESTRIX_E2E_PROJECT:-kestrix-e2e-$$}"
PROJECT_NAME="${PROJECT_NAME//[^a-zA-Z0-9_-]/-}"
BASE_PORT="${KESTRIX_E2E_PORT:-$((28080 + ($$ % 1000)))}"
BUSINESS_PORT="${KESTRIX_E2E_BUSINESS_PORT:-$((BASE_PORT + 2))}"
POSTGRES_PORT="${KESTRIX_E2E_POSTGRES_PORT:-$((BASE_PORT + 4))}"
REDIS_PORT="${KESTRIX_E2E_REDIS_PORT:-$((BASE_PORT + 5))}"
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
MASTER_KEY_FILE="${TEMP_DIR}/master.key"

mkdir -p "${REMOTE_WORK}/ci"
git init --quiet --bare --initial-branch=main "${REMOTE_BARE}"
git -C "${REMOTE_WORK}" init --quiet --initial-branch=main
git -C "${REMOTE_WORK}" remote add origin "${REMOTE_BARE}"
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
  '}' > "${REMOTE_WORK}/ci/pipeline.tf"
printf '%s\n' 'docker-e2e-source' > "${REMOTE_WORK}/version.txt"
git -C "${REMOTE_WORK}" add .
git -C "${REMOTE_WORK}" -c user.name='Kestrix Docker E2E' -c user.email=e2e@example.test commit --quiet -m initial
git -C "${REMOTE_WORK}" push --quiet -u origin main

dd if=/dev/urandom of="${MASTER_KEY_FILE}" bs=32 count=1 status=none
chmod 600 "${MASTER_KEY_FILE}"
printf '%s\n' \
  'services:' \
  '  kestrix:' \
  '    entrypoint: ["/bin/sh", "-c"]' \
  '    command: ["git config --global --add safe.directory /e2e/remote.git && exec /kestrix serve --addr :8080 --static ./ui/dist"]' \
  '    volumes:' \
  '      - source_cache:/var/lib/kestrix/cache/git' \
  "      - ${REMOTE_BARE}:/e2e/remote.git:ro" > "${OVERRIDE_FILE}"

export KESTRIX_IMAGE="${IMAGE_OVERRIDE:-${IMAGE_TAG}}"
export KESTRIX_MASTER_KEY_PATH="${MASTER_KEY_FILE}"
export KESTRIX_PORT="${BASE_PORT}"
export BUSINESS_PORT
export BUSINESS_BIND_HOST=127.0.0.1
export POSTGRES_PORT
export REDIS_PORT

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

assert_json_endpoint() {
  local path="$1"
  local expected_field="$2"
  local response_file="${TEMP_DIR}/response"
  local status
  status="$(curl -sS -o "${response_file}" -w '%{http_code}' --max-time 10 "http://127.0.0.1:${BASE_PORT}${path}")"
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
status="$(curl -sS -o "${CREDENTIAL_RESPONSE}" -w '%{http_code}' --max-time 10 \
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

PROJECT_RESPONSE="${TEMP_DIR}/project.json"
status="$(curl -sS -o "${PROJECT_RESPONSE}" -w '%{http_code}' --max-time 20 \
  -X POST "${API_BASE}/api/projects" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"docker-remote\",\"repo_url\":\"file:///e2e/remote.git\",\"repo_ref\":\"main\",\"repo_credential_ref\":\"${CREDENTIAL_REF}\",\"pipeline_dir\":\"ci\",\"pipeline_file\":\"pipeline.tf\"}")"
if [[ "${status}" != "201" ]]; then
  echo "Remote project creation returned HTTP ${status}:" >&2
  sed -n '1,120p' "${PROJECT_RESPONSE}" >&2
  exit 1
fi
if grep -Eq "${SECRET_VALUE}|repo_path|source-session|/var/lib/kestrix/cache" "${PROJECT_RESPONSE}"; then
  echo "Project API response exposed secret or host/cache path data." >&2
  exit 1
fi

PIPELINE_BEFORE="${TEMP_DIR}/pipeline-before.tf"
status="$(curl -sS -o "${PIPELINE_BEFORE}" -w '%{http_code}' --max-time 20 "${API_BASE}/api/projects/docker-remote/pipeline")"
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
status="$(curl -sS -o "${TEMP_DIR}/pipeline-after.tf" -w '%{http_code}' --max-time 20 "${API_BASE}/api/projects/docker-remote/pipeline")"
if [[ "${status}" != "200" ]]; then
  echo "Cached pipeline read failed after restart (HTTP ${status})." >&2
  exit 1
fi

status="$(curl -sS -o "${TEMP_DIR}/revoke-response" -w '%{http_code}' --max-time 10 \
  -X POST "${API_BASE}/api/credentials/${CREDENTIAL_REF}/revoke")"
if [[ "${status}" != "204" ]]; then
  echo "Credential revocation returned HTTP ${status}." >&2
  exit 1
fi
status="$(curl -sS -o "${TEMP_DIR}/revoked-read.json" -w '%{http_code}' --max-time 20 "${API_BASE}/api/projects/docker-remote/pipeline")"
if [[ "${status}" != "400" ]] || ! grep -qi 'revoked' "${TEMP_DIR}/revoked-read.json"; then
  echo "Cached source did not fail closed after credential revocation (HTTP ${status})." >&2
  exit 1
fi

curl -fsS --max-time 10 "${API_BASE}/api/credentials" > "${TEMP_DIR}/credentials.json"
curl -fsS --max-time 10 "${API_BASE}/api/projects/docker-remote" > "${TEMP_DIR}/project-read.json"
compose logs --no-color > "${TEMP_DIR}/compose.log"
if grep -R -q "${SECRET_VALUE}" "${CREDENTIAL_RESPONSE}" "${PROJECT_RESPONSE}" "${TEMP_DIR}/credentials.json" "${TEMP_DIR}/project-read.json" "${TEMP_DIR}/revoked-read.json" "${TEMP_DIR}/compose.log"; then
  echo "Credential secret was found in an API response or deployment log." >&2
  exit 1
fi

echo "=== Docker deployment E2E passed ==="
