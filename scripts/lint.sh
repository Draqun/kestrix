#!/bin/sh
set -euo pipefail

echo "=== Lint ==="

echo "--- gofmt ---"
UNFORMATTED=$(gofmt -l .)
if [ -n "$UNFORMATTED" ]; then
  echo "Files need formatting:"
  echo "$UNFORMATTED"
  exit 1
fi
echo "gofmt: OK"

echo "--- go vet ---"
go vet ./...
echo "go vet: OK"

echo "--- staticcheck ---"
if command -v staticcheck >/dev/null 2>&1; then
  staticcheck ./...
  echo "staticcheck: OK"
else
  echo "staticcheck: skipped (not installed)"
fi

echo "=== Lint passed ==="
