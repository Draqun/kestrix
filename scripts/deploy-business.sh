#!/bin/bash
set -euo pipefail

echo "=== Deploy Business Website ==="

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

SOURCE="${BUSINESS_SOURCE:-${REPO_ROOT}/ui/dist/business}"
TARGET="${BUSINESS_TARGET:-/var/www/business}"
DEPLOY_HOST="${DEPLOY_HOST:-}"
COMPOSE_FILE="${BUSINESS_COMPOSE_FILE:-${REPO_ROOT}/docker/docker-compose.prod.yml}"
BUSINESS_SERVICE="${BUSINESS_SERVICE:-business}"

if [ ! -d "$SOURCE" ]; then
  echo "Source directory ${SOURCE} not found — run 'bash scripts/build-business.sh' first"
  exit 1
fi

echo "Source: ${SOURCE}"
echo "Host:   ${DEPLOY_HOST:-local}"

if [ -n "$DEPLOY_HOST" ]; then
  echo "Target: ${TARGET}"
  echo "Deploying to remote host ${DEPLOY_HOST}..."
  rsync -avz --delete "${SOURCE}/" "${DEPLOY_HOST}:${TARGET}/"
else
  if ! command -v docker >/dev/null 2>&1; then
    echo "Docker is required for local deployment"
    exit 1
  fi

  echo "Deploying locally with Docker Compose..."
  echo "Compose: ${COMPOSE_FILE}"
  echo "Service: ${BUSINESS_SERVICE}"
  docker compose -f "${COMPOSE_FILE}" up -d --force-recreate "${BUSINESS_SERVICE}"
fi

echo "=== Business website deploy passed ==="
