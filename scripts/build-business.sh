#!/bin/sh
set -euo pipefail

echo "=== Business Website Build ==="

cd ui/business

if [ ! -f node_modules/.package-lock.json ]; then
  echo "Installing dependencies..."
  npm install --silent
fi

echo "Building business website..."
npx vite build

echo "Output: ui/dist/business/"
ls -la ../dist/business/ 2>/dev/null || echo "dist/business/ not found"

echo "=== Business website build passed ==="
