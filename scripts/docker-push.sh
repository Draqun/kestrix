#!/bin/bash
set -euo pipefail

echo "=== Docker Push ==="

REGISTRY="${DOCKER_REGISTRY:-}"
IMAGE="${DOCKER_IMAGE:-}"
TAG="${DOCKER_TAG:-$(git rev-parse --short HEAD 2>/dev/null || echo 'dev')}"

if [ -z "$IMAGE" ]; then
  echo "DOCKER_IMAGE is required when publishing an image; set it to an image owned by your registry"
  exit 1
fi
if [ -n "$REGISTRY" ] && [[ "$IMAGE" != */* ]]; then
  IMAGE="${REGISTRY}/${IMAGE}"
fi

FULL_IMAGE="${IMAGE}:${TAG}"
LATEST="${IMAGE}:latest"

if [ -n "${DOCKER_USERNAME:-}" ] && [ -n "${DOCKER_PASSWORD:-}" ]; then
  LOGIN_REGISTRY="${REGISTRY:-docker.io}"
  echo "Logging in to ${LOGIN_REGISTRY}..."
  echo "${DOCKER_PASSWORD}" | docker login "${LOGIN_REGISTRY}" -u "${DOCKER_USERNAME}" --password-stdin
fi

echo "Pushing ${FULL_IMAGE}..."
docker push "${FULL_IMAGE}"

if [ -z "${SKIP_LATEST:-}" ]; then
  echo "Pushing ${LATEST}..."
  docker push "${LATEST}"
else
  echo "Skipping ${LATEST} (SKIP_LATEST set)"
fi

echo "=== Docker push passed ==="
