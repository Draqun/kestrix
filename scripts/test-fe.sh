#!/bin/sh
set -euo pipefail

echo "=== Frontend Tests ==="

cd ui

if [ ! -d node_modules ]; then
  echo "Installing dependencies..."
  npm install --silent
fi

echo "--- Unit tests ---"
node --test src/components/Pagination.test.js
node --test src/lib/runFilters.test.js

echo "=== Frontend tests passed ==="
