#!/bin/bash
set -euo pipefail

echo "=== Docker Build ==="

REGISTRY="${DOCKER_REGISTRY:-}"
IMAGE="${DOCKER_IMAGE:-kestrix}"
TAG="${DOCKER_TAG:-$(git rev-parse --short HEAD 2>/dev/null || echo 'dev')}"
DOCKERFILE="${DOCKERFILE:-docker/Dockerfile}"

if [ -n "$REGISTRY" ] && [[ "$IMAGE" != */* ]]; then
  IMAGE="${REGISTRY}/${IMAGE}"
fi

FULL_IMAGE="${IMAGE}:${TAG}"
LATEST="${IMAGE}:latest"

echo "Registry:  ${REGISTRY:-local}"
echo "Image:     ${FULL_IMAGE}"
echo "Dockerfile: ${DOCKERFILE}"

docker build \
  --build-arg VERSION="${TAG}" \
  -t "${FULL_IMAGE}" \
  -t "${LATEST}" \
  -f "${DOCKERFILE}" \
  .

echo "Built: ${FULL_IMAGE}"
echo "Built: ${LATEST}"
echo "=== Docker build passed ==="
