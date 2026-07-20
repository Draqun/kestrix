#!/bin/sh
set -euo pipefail

echo "=== Test ==="

go test ./... -v -count=1 -coverprofile=coverage.out -covermode=atomic

if [ -f coverage.out ]; then
  echo ""
  echo "--- Coverage ---"
  go tool cover -func=coverage.out | tail -1
fi

echo "=== Test passed ==="
