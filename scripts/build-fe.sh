#!/usr/bin/env bash
set -euo pipefail

echo "=== Frontend Build ==="

cd ui

if [ ! -d node_modules ]; then
  echo "Installing dependencies..."
  npm install --silent
fi

echo "Building frontend..."
npx vite build

echo "Output: ui/dist/"
ls -la dist/ 2>/dev/null || echo "dist/ not found"

echo "=== Frontend build passed ==="
