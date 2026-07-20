#!/usr/bin/env bash
set -euo pipefail

echo "=== Deploy ==="

REGISTRY="${DOCKER_REGISTRY:-registry.gitlab.com}"
IMAGE="${DOCKER_IMAGE:-${REGISTRY}/lunaris-bot/metaci}"
TAG="${DOCKER_TAG:-$(git rev-parse --short HEAD 2>/dev/null || echo 'dev')}"
COMPOSE_DIR="${COMPOSE_DIR:-docker}"
DEPLOY_HOST="${DEPLOY_HOST:-}"
HEALTH_URL="${HEALTH_URL:-http://localhost:8080/health}"

FULL_IMAGE="${IMAGE}:${TAG}"

export METACI_IMAGE="${FULL_IMAGE}"

echo "Image:   ${FULL_IMAGE}"
echo "Compose: ${COMPOSE_DIR}"
echo "Host:    ${DEPLOY_HOST:-local}"

# Push to registry if credentials are available.
if [ -n "${DOCKER_USERNAME:-}" ] && [ -n "${DOCKER_PASSWORD:-}" ]; then
  echo "Logging in to ${REGISTRY}..."
  echo "${DOCKER_PASSWORD}" | docker login "${REGISTRY}" -u "${DOCKER_USERNAME}" --password-stdin
  echo "Pushing ${FULL_IMAGE}..."
  docker push "${FULL_IMAGE}"
fi

if [ -n "$DEPLOY_HOST" ]; then
  echo "Deploying to remote host ${DEPLOY_HOST}..."
  ssh "${DEPLOY_HOST}" "cd $(pwd) && docker compose -f ${COMPOSE_DIR}/docker-compose.yml pull && docker compose -f ${COMPOSE_DIR}/docker-compose.yml up -d"
else
  echo "Deploying locally..."
  docker compose -f "${COMPOSE_DIR}/docker-compose.yml" pull 2>/dev/null || true
  docker compose -f "${COMPOSE_DIR}/docker-compose.yml" up -d
fi

echo "Waiting for health check..."
RETRIES=10
until curl -sf "${HEALTH_URL}" >/dev/null 2>&1; do
  RETRIES=$((RETRIES - 1))
  if [ "$RETRIES" -le 0 ]; then
    echo "Health check failed after retries"
    exit 1
  fi
  sleep 2
done

echo "Health check: OK"
echo "=== Deploy passed ==="
