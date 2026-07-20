#!/usr/bin/env bash
set -euo pipefail

echo "=== Docker Build ==="

REGISTRY="${DOCKER_REGISTRY:-registry.gitlab.com}"
IMAGE="${DOCKER_IMAGE:-${REGISTRY}/lunaris-bot/metaci}"
TAG="${DOCKER_TAG:-$(git rev-parse --short HEAD 2>/dev/null || echo 'dev')}"
DOCKERFILE="${DOCKERFILE:-docker/Dockerfile}"

FULL_IMAGE="${IMAGE}:${TAG}"
LATEST="${IMAGE}:latest"

echo "Registry:  ${REGISTRY}"
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
