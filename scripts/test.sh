#!/bin/bash
set -euo pipefail

echo "=== Test ==="

PKGS=$(go list ./... | grep -v '/docs')
go test $PKGS -v -count=1 -coverprofile=coverage.out -covermode=atomic -race

echo "=== Coverage check ==="

bash scripts/test_coverage.sh coverage.out

echo "=== Test passed ==="
