#!/bin/sh
set -euo pipefail

echo "=== Docker Push ==="

REGISTRY="${DOCKER_REGISTRY:-registry.gitlab.com}"
IMAGE="${DOCKER_IMAGE:-${REGISTRY}/lunaris-bot/metaci}"
TAG="${DOCKER_TAG:-$(git rev-parse --short HEAD 2>/dev/null || echo 'dev')}"

FULL_IMAGE="${IMAGE}:${TAG}"
LATEST="${IMAGE}:latest"

if [ -n "${DOCKER_USERNAME:-}" ] && [ -n "${DOCKER_PASSWORD:-}" ]; then
  echo "Logging in to ${REGISTRY}..."
  echo "${DOCKER_PASSWORD}" | docker login "${REGISTRY}" -u "${DOCKER_USERNAME}" --password-stdin
fi

echo "Pushing ${FULL_IMAGE}..."
docker push "${FULL_IMAGE}"

echo "Pushing ${LATEST}..."
docker push "${LATEST}"

echo "=== Docker push passed ==="
