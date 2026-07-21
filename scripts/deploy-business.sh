#!/bin/bash
set -euo pipefail

echo "=== Deploy Business Website ==="

SOURCE="${BUSINESS_SOURCE:-ui/dist/business}"
TARGET="${BUSINESS_TARGET:-/var/www/business}"
DEPLOY_HOST="${DEPLOY_HOST:-}"

if [ ! -d "$SOURCE" ]; then
  echo "Source directory $SOURCE not found — run build first"
  exit 1
fi

echo "Source: ${SOURCE}"
echo "Target: ${TARGET}"
echo "Host:   ${DEPLOY_HOST:-local}"

if [ -n "$DEPLOY_HOST" ]; then
  echo "Deploying to remote host ${DEPLOY_HOST}..."
  rsync -avz --delete "${SOURCE}/" "${DEPLOY_HOST}:${TARGET}/"
else
  echo "Deploying locally to ${TARGET}..."
  mkdir -p "${TARGET}"
  cp -r "${SOURCE}/"* "${TARGET}/"
fi

echo "=== Business website deploy passed ==="
