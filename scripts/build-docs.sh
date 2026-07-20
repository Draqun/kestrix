#!/bin/sh
set -euo pipefail

echo "=== Build Documentation ==="

# Install go-mkdocs if not already available
if ! command -v mkdocs >/dev/null 2>&1; then
  echo "Installing go-mkdocs..."
  go install github.com/grokify/go-mkdocs/v2/cmd/mkdocs@latest
  # Ensure $GOPATH/bin is in PATH for this session
  export PATH="$PATH:$(go env GOPATH)/bin"
fi

# Generate static site from markdown in ./docs into ./ui/dist/docs
mkdocs -source ./docs -output ./ui/dist/docs

echo "=== Documentation built into ui/dist/docs ==="
