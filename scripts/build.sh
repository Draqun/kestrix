#!/usr/bin/env bash
set -euo pipefail

echo "=== Build ==="

VERSION="${METACI_VERSION:-dev}"
COMMIT="${METACI_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')}"
BUILD_DIR="${METACI_BUILD_DIR:-bin}"

mkdir -p "$BUILD_DIR"

LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}"

echo "Building metaci ${VERSION} (${COMMIT})..."
CGO_ENABLED=0 go build -ldflags="${LDFLAGS}" -o "${BUILD_DIR}/metaci" ./cmd/metaci

echo "Binary: ${BUILD_DIR}/metaci"
echo "=== Build passed ==="
