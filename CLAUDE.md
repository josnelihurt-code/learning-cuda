# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

CUDA Learning Platform - Real-time image/video processing via CUDA GPU kernels. Go web server communicates with C++/CUDA accelerator library via gRPC for distributed processing.

## Build & Development Commands

### Building Components
```bash
# C++ (Bazel) — requires TensorRT dev headers installed
bazel build //src/cpp_accelerator/...

# Go (from src/go_api/)
cd src/go_api && make build

# Frontend (from src/front-end/)
cd src/front-end && npm install && npm run dev
```

### Testing
```bash
# All coverage tests
./scripts/test/coverage.sh

# Unit tests
./scripts/test/unit-tests.sh
./scripts/test/unit-tests.sh --skip-golang   # Frontend only
./scripts/test/unit-tests.sh --skip-frontend # Go only

# Individual test suites
go test -race ./src/go_api/pkg/...                 # Go tests
cd src/front-end && npm run test                   # Frontend (Vitest)
bazel test //src/cpp_accelerator/...                # C++ tests

# Specific C++ test
bazel test //src/cpp_accelerator/core:logger_test

# E2E tests
./scripts/test/e2e.sh --chromium   # Fast: Chromium only
./scripts/test/e2e.sh              # All browsers
```

### Linting
```bash
./scripts/test/linters.sh          # All linters
./scripts/test/linters.sh --fix    # Auto-fix
```

## Pull Requests: merge-me & Stacked PRs

### merge-me label

Labeling a PR `merge-me` is standing intent ("merge when green"), not a command.
The `merge-me` workflow (`.github/workflows/merge-me.yml`, powered by the shared
composite action `josnelihurt/code.examples.ci/merge-me`, pinned by SHA) re-evaluates
on real events only — label added/removed, push, reopen, either Docker Monorepo CI
workflow completing (x86 and ARM64), or manual dispatch. When both CI workflows are
green it merges via GitHub's asynchronous merge endpoint; when checks are pending on
an ordinary PR it arms GitHub's server-side auto-merge. Removing the label disarms
any armed auto-merge. All merges are **squash**. The workflow name must stay
`merge-me` (the action self-excludes its own checks by name).

### Stacked PRs

Multi-unit changes ship one branch/PR per unit, each based on the previous,
managed with the `gh stack` extension (`github/gh-stack`; install with
`gh extension install github/gh-stack`, invoked as `gh stack`):

- **Fresh stack**: `gh stack init <bottom> … <top>` (or `gh stack init` to start
  from the current branch), commit per layer, then `gh stack submit --auto --open`.
- **Existing PRs**: `gh stack link <bottom-pr> … <top-pr>` chains them on GitHub
  and retargets their bases — no local stack state needed.
- **Stack PRs are created ready for review — never drafts.** The only exception is
  a genuinely blocked unit, which starts as a draft and is flipped with `gh pr ready`
  once the block clears.
- **Merging a stack**: `gh stack merge [<pr>]` — atomic, all-or-nothing up to
  that PR (squash). Manual bottom-up also works; GitHub auto-retargets the next
  PR when the one below merges. The `merge-me` label works for ordinary PRs
  (base `main`), but **not** for upper stack layers: the Docker CI workflows
  only trigger on PRs targeting `main`, so layers with intermediate bases never
  report checks and merge-me holds them forever. Fixing that would require
  widening the CI `pull_request` triggers to non-main bases.
- After a squash-merge lands a lower layer, run `gh stack sync` (cascading rebase)
  before continuing work upstack — a stale branch over a squash-merged base shows
  phantom conflicts. Never force-push mid-stack branches outside `gh stack sync`.

## Architecture

### Code Structure
```
src/cpp_accelerator/
  application/         # Use cases, FilterPipeline, BufferPool
  domain/interfaces/   # IFilter, ImageBuffer, IImageProcessor
  adapters/compute/cuda/ # CUDA kernel implementations
  adapters/compute/cpu/  # CPU fallback implementations
  adapters/grpc_control/ # gRPC client (primary integration)
  adapters/webrtc/       # WebRTC (data channel framing, live video)
  cmd/accelerator_control_client/ # Client binary
  core/               # Logger, Telemetry, Result type

src/go_api/
  cmd/server/         # main.go entry point
  pkg/app/            # Application bootstrap
  pkg/application/    # Use cases
  pkg/domain/         # Domain logic
  pkg/infrastructure/ # Repositories, gRPC client
  pkg/interfaces/     # HTTP/ConnectRPC/WebRTC signaling handlers

src/front-end/        # React (Vite)
```


### Key Patterns
- Clean Architecture with dependency injection
- FilterPipeline orchestrates composable filter chains
- ProcessorEngine coordinates between ports and pipeline
- gRPC streaming for control/signaling (AcceleratorControlService.Connect bidi stream); media over WebRTC

## Testing Patterns

### Go Tests
- Use AAA comments (Arrange/Act/Assert)
- Use `testify/mock` (embed `mock.Mock`)
- Use `sut` for system under test
- Table-driven tests with `assertResult` function
- Naming: `Success_`, `Error_`, `Edge_` prefix
- Test data builders: `makeXXX()`

### TypeScript Tests (Vitest)
- Use AAA comments
- Use `vi.fn()` and `vi.mock()`
- Use `sut` for system under test
- Table-driven tests for multiple cases
- Naming: `Success_`, `Error_`, `Edge_` prefix
- Test data builders: `makeXXX()`

### C++ Tests (GoogleTest)
- Bazel test targets: `//src/cpp_accelerator/path:target_test`
- Equivalence tests verify CPU/CUDA produce identical results

## Configuration

- Development: `config/config.dev.yaml`
- Staging: `config/config.staging.yaml`
- Production: `config/config.production.yaml`

Proto generation: `./scripts/build/protos.sh`

## Tech Stack

- **Backend**: Go with native HTTPS, WebRTC signaling via ConnectRPC
- **Processing**: C++/CUDA via gRPC service (ConnectRPC)
- **Build**: Bazel for C++/CUDA, Makefile for Go
- **Frontend**: React + TypeScript with Vite
- **Observability**: OpenTelemetry, Jaeger tracing, Grafana dashboards, Loki logs

## TensorRT Setup (YOLO inference)

YOLO detection uses TensorRT. TensorRT dev headers must be installed — there is no stub fallback.

**Minimum supported version: TRT 10.0** — both target platforms run TRT 10.x:
- Jetson Nano Orin (JetPack 6 / R36): TRT 10.3, CUDA 12.6
- Dev PC (x86, RTX 4000): TRT 10.x, CUDA 12.5+

### x86 Ubuntu — install CUDA 12.9 + TensorRT dev headers

The TRT packages must match the CUDA version your driver supports.  
Driver 575.x supports **CUDA 12.9** — use the `+cuda12.9` TRT variant:

```bash
# If you accidentally have +cuda13.2 TRT (createInferBuilder error 35), run:
sudo bash scripts/dev/fix-cuda-trt-versions.sh

# Fresh install:
sudo apt-get install -y cuda-toolkit-12-9 \
  libnvinfer-dev=10.16.1.11-1+cuda12.9 \
  libnvonnxparsers-dev=10.16.1.11-1+cuda12.9
# Verify
dpkg -l | grep libnvinfer
```

### Build
```bash
bazel build //src/cpp_accelerator/...
```

### x86 USB camera support (for testing remote camera feature locally)

Requires GStreamer dev headers and plugins:
```bash
sudo apt-get install -y libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev \
    gstreamer1.0-plugins-good libgstreamer-plugins-ugly1.0-dev
```

Build and run with v4l2 camera support enabled:
```bash
./scripts/dev/start.sh --build --accelerator=full,v4l2-camera
```

Without `--config=v4l2-camera` the camera streaming stub is used — cameras are
detected and listed in the UI, but `StartCameraStream` logs a warning and returns
false (no video).

### Dev stack
```bash
./scripts/dev/start.sh --build   # builds then starts all services (stub camera)
./scripts/dev/start.sh --build --accelerator=full,v4l2-camera  # with USB cameras
```

### Jetson Nano Orin / JetPack 6
TensorRT 10.x is pre-installed with JetPack 6. Build normally:
```bash
bazel build //src/cpp_accelerator/...
```
The code targets the TRT 10.x API exclusively: `getNbIOTensors`, `setTensorAddress`,
`enqueueV3`, `setMemoryPoolLimit`, and `buildSerializedNetwork`.

### ONNX → TRT engine caching
On first run with a new model, the detector builds a TRT engine from the `.onnx` file
and saves it as `.engine` (or `.jp6.engine` on Jetson Orin / aarch64). Subsequent runs
load the cached engine directly. `processor_engine.cpp` references the model by
id (`yolov10n`); the model path `data/models/yolov10n.onnx` is registered in
`src/cpp_accelerator/composition/platform/cuda/cuda_platform.cpp`.

### Docker builds with TRT runtime
Pass `--build-arg ENABLE_TENSORRT=true` to include TRT runtime libs in the container:
```bash
docker build --build-arg ENABLE_TENSORRT=true -f src/cpp_accelerator/Dockerfile.build .
```

