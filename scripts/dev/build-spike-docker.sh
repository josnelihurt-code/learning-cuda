#!/usr/bin/env bash
# Build spike_opencl_mock with Bazel, stage it for the mock image, then build both spike Docker images.
# Run from any directory; requires docker, bazel, and (for spike-cuda) registry access to intermediate images.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT}"

SPIKE_PKG="//src/cpp_accelerator/cmd/spike_multi_gpu_backend"
SPIKE_DIR="${ROOT}/src/cpp_accelerator/cmd/spike_multi_gpu_backend"
ARTIFACTS="${SPIKE_DIR}/docker/artifacts"
COMPOSE="${SPIKE_DIR}/docker-compose.spike.yml"

echo "Building ${SPIKE_PKG}:spike_opencl_mock (host)..."
bazel build "${SPIKE_PKG}:spike_opencl_mock"

mkdir -p "${ARTIFACTS}"
BIN_MOCK="${ROOT}/$(bazel info bazel-bin)/src/cpp_accelerator/cmd/spike_multi_gpu_backend/spike_opencl_mock"
cp -L "${BIN_MOCK}" "${ARTIFACTS}/spike_opencl_mock"
echo "Staged mock binary to ${ARTIFACTS}/spike_opencl_mock"

echo "Building Docker images cpp-spike-opencl-mock:local and cpp-spike-cuda:local..."
docker compose -f "${COMPOSE}" build spike-mock
docker compose -f "${COMPOSE}" build spike-cuda

echo "Done. Examples:"
echo "  docker compose -f ${COMPOSE} run --rm spike-mock"
echo "  docker compose -f ${COMPOSE} run --rm spike-cuda   # needs NVIDIA Container Toolkit + GPU"
