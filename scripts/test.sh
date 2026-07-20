#!/bin/bash
set -euo pipefail

echo "=== Test ==="

go test ./... -v -count=1

echo "=== Test passed ==="
