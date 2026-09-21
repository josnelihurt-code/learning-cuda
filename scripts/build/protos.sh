#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Toolchain versions for the bufgen image come from the repo-wide source of truth.
if [ ! -f "$PROJECT_ROOT/versions.env" ]; then
    echo "FAILED: versions.env not found at repo root"
    exit 1
fi
. "$PROJECT_ROOT/versions.env"

cd "$PROJECT_ROOT"

CONTAINER_CMD="podman"
# CONTAINER_CMD="docker"

IMAGE_NAME="cuda-learning-bufgen:latest"
DOCKERFILE="bufgen.dockerfile"

echo "Building Protocol Buffers..."

if ! $CONTAINER_CMD image exists "$IMAGE_NAME" 2>/dev/null; then
    echo "Building bufgen Docker image..."
    $CONTAINER_CMD build \
        --build-arg "GO_VERSION=${GO_VERSION}" \
        --build-arg "ALPINE_VERSION=${ALPINE_VERSION}" \
        -t "$IMAGE_NAME" -f "$DOCKERFILE" . || {
        echo "FAILED: Could not build bufgen image"
        exit 1
    }
    echo "Bufgen image built successfully"
fi

echo "Generating proto files..."
mkdir -p "$PROJECT_ROOT/proto/gen/genconnect" "$PROJECT_ROOT/src/front-end/src/gen"
CACHE_DIR="$PROJECT_ROOT/.cache/buf-$(id -u)"
mkdir -p "$CACHE_DIR"
chmod -R 755 "$PROJECT_ROOT/proto/gen" "$CACHE_DIR" 2>/dev/null || true
$CONTAINER_CMD run --rm \
    -v "$PROJECT_ROOT:/workspace" \
    -u $(id -u):$(id -g) \
    -e XDG_CACHE_HOME=/workspace/.cache/buf-$(id -u) \
    -e PATH=/usr/local/bin:/usr/bin:/bin \
    --userns=keep-id \
    "$IMAGE_NAME" generate --template buf.gen.yaml || {
    echo "FAILED: Proto generation failed"
    exit 1
}
chown -R $(id -u):$(id -g) "$PROJECT_ROOT/proto/gen" "$PROJECT_ROOT/src/front-end/src/gen" 2>/dev/null || true

echo "Proto files generated successfully"
echo "  Go:         proto/gen/*.pb.go"
echo "  TypeScript: src/front-end/src/gen/*.ts"

