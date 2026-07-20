#!/bin/sh
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

# Stop dev container if running (free port 8080).
docker stop metaci-dev 2>/dev/null || true
docker rm metaci-dev 2>/dev/null || true

if [ -n "$DEPLOY_HOST" ]; then
  echo "Deploying to remote host ${DEPLOY_HOST}..."
  scp "${COMPOSE_DIR}/docker-compose.prod.yml" "${DEPLOY_HOST}:~/metaci-docker-compose.yml"
  ssh "${DEPLOY_HOST}" "METACI_IMAGE=${FULL_IMAGE} docker compose -f ~/metaci-docker-compose.yml pull 2>/dev/null || true && METACI_IMAGE=${FULL_IMAGE} docker compose -f ~/metaci-docker-compose.yml up -d"
else
  echo "Deploying locally..."
  METACI_IMAGE="${FULL_IMAGE}" docker compose -f "${COMPOSE_DIR}/docker-compose.prod.yml" up -d
fi

echo "Waiting for health check..."
RETRIES=15
until curl -sf "${HEALTH_URL}" >/dev/null 2>&1; do
  RETRIES=$((RETRIES - 1))
  if [ "$RETRIES" -le 0 ]; then
    echo "Health check failed after retries"
    docker logs metaci-prod 2>&1 | tail -20 || true
    exit 1
  fi
  sleep 2
done

echo "Health check: OK"
echo "=== Deploy passed ==="
