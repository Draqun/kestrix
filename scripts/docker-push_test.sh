#!/bin/bash
set -euo pipefail

# docker-push_test.sh — tests for docker-push.sh
PASS_COUNT=0
FAIL_COUNT=0

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DOCKER_PUSH="$SCRIPT_DIR/docker-push.sh"

TMPDIR=$(mktemp -d)
cleanup() { rm -rf "$TMPDIR"; }
trap cleanup EXIT

pass() { echo "PASS: $1"; PASS_COUNT=$((PASS_COUNT + 1)); }
fail() { echo "FAIL: $1"; FAIL_COUNT=$((FAIL_COUNT + 1)); }

# Test 1: Refuses to run when DOCKER_IMAGE is unset
set +e
output=$(DOCKER_IMAGE="" bash "$DOCKER_PUSH" 2>&1)
rc=$?
set -e
if [ $rc -ne 0 ] && echo "$output" | grep -q "DOCKER_IMAGE is required"; then
  pass "fails without DOCKER_IMAGE"
else
  fail "expected failure without DOCKER_IMAGE, got exit $rc: $output"
fi

# Create a mock docker command in PATH to record calls
MOCK_BIN="$TMPDIR/bin"
mkdir -p "$MOCK_BIN"
DOCKER_LOG="$TMPDIR/docker.log"

cat <<'EOF' > "$MOCK_BIN/docker"
#!/bin/bash
echo "$@" >> "$MOCK_DOCKER_LOG"
if [ "$1" = "login" ]; then
  cat >/dev/null # consume stdin password
fi
exit 0
EOF
chmod +x "$MOCK_BIN/docker"

# Test 2: Defaults registry to docker.io on credential login when DOCKER_REGISTRY is empty
: > "$DOCKER_LOG"
PATH="$MOCK_BIN:$PATH" MOCK_DOCKER_LOG="$DOCKER_LOG" \
  DOCKER_IMAGE="draqun/kestrix" DOCKER_TAG="v1.0.0" \
  DOCKER_USERNAME="draqun" DOCKER_PASSWORD="secret" \
  bash "$DOCKER_PUSH" >/dev/null 2>&1

if grep -q "login docker.io -u draqun --password-stdin" "$DOCKER_LOG" && \
   grep -q "push draqun/kestrix:v1.0.0" "$DOCKER_LOG" && \
   grep -q "push draqun/kestrix:latest" "$DOCKER_LOG"; then
  pass "defaults to docker.io and pushes full and latest tags"
else
  fail "expected docker.io login and push, got: $(cat "$DOCKER_LOG")"
fi

# Test 3: Respects SKIP_LATEST=1
: > "$DOCKER_LOG"
PATH="$MOCK_BIN:$PATH" MOCK_DOCKER_LOG="$DOCKER_LOG" \
  DOCKER_IMAGE="draqun/kestrix" DOCKER_TAG="v1.0.0" \
  DOCKER_REGISTRY="docker.io" DOCKER_USERNAME="draqun" DOCKER_PASSWORD="secret" \
  SKIP_LATEST=1 \
  bash "$DOCKER_PUSH" >/dev/null 2>&1

if grep -q "push draqun/kestrix:v1.0.0" "$DOCKER_LOG" && \
   ! grep -q "push draqun/kestrix:latest" "$DOCKER_LOG"; then
  pass "skips latest tag when SKIP_LATEST=1"
else
  fail "expected latest tag skipped, got: $(cat "$DOCKER_LOG")"
fi

# Test 4: Custom registry prepends to image name when image has no slash
: > "$DOCKER_LOG"
PATH="$MOCK_BIN:$PATH" MOCK_DOCKER_LOG="$DOCKER_LOG" \
  DOCKER_IMAGE="kestrix" DOCKER_TAG="v1.0.0" \
  DOCKER_REGISTRY="registry.gitlab.com/group" \
  SKIP_LATEST=1 \
  bash "$DOCKER_PUSH" >/dev/null 2>&1

if grep -q "push registry.gitlab.com/group/kestrix:v1.0.0" "$DOCKER_LOG"; then
  pass "prepends custom registry when image name has no slash"
else
  fail "expected prepended registry, got: $(cat "$DOCKER_LOG")"
fi

echo ""
echo "Results: $PASS_COUNT passed, $FAIL_COUNT failed"
[ "$FAIL_COUNT" -eq 0 ]
