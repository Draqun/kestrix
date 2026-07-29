#!/bin/bash
set -euo pipefail

echo "=== Test ==="

PKGS=$(go list ./... | grep -v '/docs')
go test $PKGS -v -count=1 -coverprofile=coverage.out -covermode=atomic -race

# PostgreSQL integration tests require an explicitly configured test database.
# On a clean local checkout, exclude that adapter from the denominator; CI sets
# METACI_TEST_DATABASE_URL and therefore gates the complete store package.
if [ -z "${METACI_TEST_DATABASE_URL:-}" ]; then
  awk '!/\/internal\/store\/pgstore[^/]*\.go:/' coverage.out > coverage.local.out
  mv coverage.local.out coverage.out
fi

# CLI tests intentionally execute the built binary as a subprocess. Go's
# coverage runtime cannot attribute subprocess execution to the parent profile.
awk '!/\/cmd\/kestrix\/[^/]*\.go:/' coverage.out > coverage.testable.out
mv coverage.testable.out coverage.out

echo "=== Coverage check ==="

bash scripts/test_coverage.sh coverage.out

echo "=== Test passed ==="
