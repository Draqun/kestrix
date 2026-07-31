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
if [[ ! -f "${COMPOSE_FILE}" ]]; then
  echo "Production Compose file not found: ${COMPOSE_FILE}" >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon is unavailable. Start Docker and retry." >&2
  exit 1
fi

TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/kestrix-docker-e2e.XXXXXX")"

export KESTRIX_IMAGE="${IMAGE_OVERRIDE:-${IMAGE_TAG}}"
export KESTRIX_PORT="${BASE_PORT}"
export BUSINESS_PORT
export BUSINESS_BIND_HOST=127.0.0.1
export POSTGRES_PORT
export REDIS_PORT

compose() {
  "${COMPOSE[@]}" -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" "$@"
}

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

echo "Waiting up to ${HEALTH_TIMEOUT}s for ${HEALTH_URL}..."
deadline=$((SECONDS + HEALTH_TIMEOUT))
health_body=""
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
echo "=== Docker deployment E2E passed ==="
