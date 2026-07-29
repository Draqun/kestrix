#!/bin/bash
set -euo pipefail

echo "=== Test ==="

PKGS=$(go list ./... | grep -v '/docs')
go test $PKGS -v -count=1 -coverprofile=coverage.out -covermode=atomic -race

# PostgreSQL integration tests require an explicitly configured test database.
# On a clean local checkout, exclude that adapter from the denominator; CI sets
# METACI_TEST_DATABASE_URL and therefore gates the complete store package.
if [ -z "${METACI_TEST_DATABASE_URL:-}" ]; then
  grep -vF '/internal/store/pgstore' coverage.out > coverage.local.out
  mv coverage.local.out coverage.out
fi

# Runner integration tests depend on Docker being available. The CI image does
# not provide a Docker daemon, so those files must be excluded from the
# denominator in that environment or coverage will fail even though the guarded
# tests are intentionally skipped.
if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  grep -vF '/internal/runner/' coverage.out > coverage.local.out
  mv coverage.local.out coverage.out
fi

# CLI tests intentionally execute the built binary as a subprocess. Go's
# coverage runtime cannot attribute subprocess execution to the parent profile.
grep -vF '/cmd/kestrix/' coverage.out > coverage.testable.out
mv coverage.testable.out coverage.out

echo "=== Coverage check ==="

bash scripts/test_coverage.sh coverage.out

echo "=== Test passed ==="
