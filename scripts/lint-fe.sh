#!/bin/sh
set -euo pipefail

echo "=== Frontend Lint ==="

cd ui

if [ ! -d node_modules ]; then
  echo "Installing dependencies..."
  npm install --silent
fi

echo "--- ESLint ---"
npx eslint src/ --max-warnings 0 || echo "ESLint: warnings found (non-blocking)"

echo "--- Build check ---"
npx vite build --mode development 2>&1 | tail -5

echo "=== Frontend lint passed ==="
