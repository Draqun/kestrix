#!/bin/bash
set -euo pipefail

echo "=== Build ==="

VERSION="${KESTRIX_VERSION:-dev}"
COMMIT="${KESTRIX_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')}"
BUILD_DIR="${KESTRIX_BUILD_DIR:-bin}"

mkdir -p "$BUILD_DIR"

LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}"

echo "Building Kestrix ${VERSION} (${COMMIT})..."
CGO_ENABLED=0 go build -ldflags="${LDFLAGS}" -o "${BUILD_DIR}/kestrix" ./cmd/kestrix

echo "Binary: ${BUILD_DIR}/kestrix"
echo "=== Build passed ==="
