#!/bin/bash
set -euo pipefail

echo "=== BE Coverage ==="
go test ./... -count=1 -coverprofile=coverage.out -covermode=atomic
go tool cover -func=coverage.out
echo ""

echo "=== BE coverage complete ==="