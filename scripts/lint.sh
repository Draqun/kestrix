#!/bin/bash
set -euo pipefail

echo "=== Lint ==="

echo "--- gofmt ---"
GO_FILES=()
while IFS= read -r f; do GO_FILES+=("$f"); done < <(find . -type f -name '*.go' \
  -not -path './.gocache/*' \
  -not -path './.gomodcache/*' \
  -not -path './.gopath/*' \
  -not -path './node_modules/*' \
  -not -path './ui/dist/*')
UNFORMATTED=$(gofmt -l "${GO_FILES[@]}")
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
