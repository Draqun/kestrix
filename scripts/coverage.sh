#!/bin/bash
set -euo pipefail

echo "=== BE Coverage ==="
go test ./... -count=1 -coverprofile=coverage.out -covermode=atomic
go tool cover -func=coverage.out
echo ""

echo "=== FE Coverage ==="
cd ui
node --test --experimental-test-coverage src/components/Pagination.test.js src/lib/runFilters.test.js
echo ""

echo "=== Coverage complete ==="