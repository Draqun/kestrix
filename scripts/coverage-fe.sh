#!/bin/bash
set -euo pipefail

echo "=== FE Coverage ==="
cd ui
node --test --experimental-test-coverage src/components/Pagination.test.js src/lib/runFilters.test.js
echo ""

echo "=== FE coverage complete ==="