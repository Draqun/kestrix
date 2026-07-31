#!/bin/bash
set -euo pipefail

echo "=== Frontend Lint ==="

cd ui

if [ ! -d node_modules ]; then
  echo "Installing dependencies..."
  npm install --silent
fi

echo "--- Oxlint ---"
npm run lint

echo "--- Build check ---"
BUILD_DIR="$(mktemp -d)"
trap 'rm -rf "$BUILD_DIR"' EXIT
npx vite build --mode development --outDir "$BUILD_DIR"

echo "=== Frontend lint passed ==="
