# Docker Build Scripts

This document explains the Docker build orchestration scripts used in the CUDA Learning Platform.

## Overview

These scripts manage Docker image builds across multiple architectures (amd64/arm64) and stages. They support local builds, CI/CD automation, and image pushing to GHCR.

## Scripts

### `build-local.sh`

**Purpose**: Build Docker images sequentially with stage targeting.

**Usage**:
```bash
./scripts/docker/build-local.sh [options]

Options:
  --arch <arch>          Target architecture (amd64 or arm64). Defaults to host arch.
  --registry <name>      Registry prefix for tags. Defaults to $REGISTRY or "local".
  --base-prefix <p>      Image namespace following the registry.
  --stage <name>         Build only the specified stage (can be passed multiple times).
  --list-stages          Print available stages and exit.
  -h, --help             Show help message.
```

**Available Stages** (in build order):
```
proto-tools
go-builder
bazel-base
cpp-dependencies
cuda-runtime
yolo-tools
yolo-model
runtime-base
integration-base
proto
cpp-builder
golang
app
cpp-accelerator
web-frontend
```

**Environment Variables**:
| Variable | Default | Description |
|----------|---------|-------------|
| `REGISTRY` | `local` | Docker registry |
| `BASE_IMAGE_PREFIX` | `josnelihurt-code/learning-cuda` | Image namespace |
| `BAZEL_REMOTE_CACHE` | Auto-detected | Bazel remote cache endpoint |

Note: the `ARCH` environment variable is ignored — the script derives the default architecture from `uname -m`; use the `--arch` flag to override it.

**Build Behavior**:
- Stages build in dependency order
- Each stage creates versioned and `latest` tags
- Validates base images exist before building dependents
- Auto-detects bazel-remote cache on LAN

**Example**:
```bash
# Build only app stage locally
./scripts/docker/build-local.sh --stage app

# Build all stages for amd64 and push to GHCR
REGISTRY=ghcr.io ./scripts/docker/build-local.sh --arch amd64

# Build specific stages for ARM64
./scripts/docker/build-local.sh --arch arm64 \
  --stage cuda-runtime \
  --stage cpp-builder \
  --stage cpp-accelerator
```

**Version File Reading**:
Each stage reads a `VERSION` file to tag images:
```bash
src/cpp_accelerator/VERSION          → cpp-accelerator:cpp-accelerator-1.2.3-proto0.9.0-amd64
src/go_api/VERSION                   → app:1.2.3-amd64
src/front-end/VERSION                → web-frontend:fe-1.2.3-...
src/cpp_accelerator/docker-cuda-runtime/VERSION  → cuda-runtime:1.2.3-amd64
```

---

### `pull-ghcr-cpp-intermediates.sh`

**Purpose**: Pull pre-built intermediate images from GHCR to avoid rebuilding.

**Usage**:
```bash
PULL_PROTO_LATEST=0|1 \
PULL_CPP_DEPENDENCIES=0|1 \
PULL_CUDA_RUNTIME=0|1 \
ARCH=arm64 \
./scripts/docker/pull-ghcr-cpp-intermediates.sh
```

**Environment Variables**:
| Variable | Default | Description |
|----------|---------|-------------|
| `REGISTRY` | `ghcr.io` | Docker registry |
| `BASE_IMAGE_PREFIX` | `josnelihurt-code/learning-cuda` | Image namespace |
| `ARCH` | `arm64` | Target architecture |
| `PULL_PROTO_LATEST` | `0` | Pull proto-generated if 1 |
| `PULL_CPP_DEPENDENCIES` | `0` | Pull cpp-dependencies if 1 |
| `PULL_CUDA_RUNTIME` | `0` | Pull cuda-runtime if 1 |

**Behavior**:
- Skips images already present locally
- Creates `latest` alias from versioned tag
- Used by CI to avoid rebuilding unchanged intermediates

**Images Pulled**:
```bash
proto-generated-latest-${ARCH}
cpp-dependencies-latest-${ARCH}
cuda-runtime-latest-${ARCH}
```

---

### `push-tagged-images.sh`

**Purpose**: Push all tagged images to GHCR and ensure `latest-${ARCH}` aliases exist.

**Usage**:
```bash
LATEST_ALIASES="app cpp-accelerator web-frontend" \
ARCH=amd64 \
REGISTRY=ghcr.io \
./scripts/docker/push-tagged-images.sh
```

**Environment Variables**:
| Variable | Default | Description |
|----------|---------|-------------|
| `REGISTRY` | `ghcr.io` | Docker registry |
| `BASE_IMAGE_PREFIX` | `josnelihurt-code/learning-cuda` | Image namespace |
| `ARCH` | `amd64` | Target architecture |
| `LATEST_ALIASES` | `app cpp-accelerator web-frontend` | Images to tag as latest |

**Behavior**:
1. Pushes all images matching the registry prefix
2. Ensures `latest-${ARCH}` tags exist
3. Repairs missing `latest-${ARCH}` from versioned tags

**Repair Logic**:
For images without a `latest-amd64` tag, the script:
1. Finds the highest versioned tag for that image
2. Creates a `latest-amd64` tag
3. Pushes the alias

**Supported Repair Patterns**:
| Image | Tag Pattern |
|-------|-------------|
| `app` | `app:X.Y.Z-amd64` |
| `web-frontend` | `web-frontend:fe-X.Y.Z-...` |
| `cpp-accelerator` | `cpp-accelerator:cpp-accelerator-X.Y.Z-...` |
| `yolo-model-gen` | `yolo-model-gen:X.Y.Z-amd64` |

**Exit Codes**:
- `0`: Success
- `1`: Failed to publish one or more `latest-${ARCH}` aliases

---

### `generate-certs.sh`

**Purpose**: Generate a self-signed localhost TLS certificate for local HTTPS development (runs `openssl` inside a `podman` container).

**Usage**:
```bash
./scripts/docker/generate-certs.sh
```

**Outputs** (written to `.secrets/`):
- `localhost+2.pem` - Self-signed certificate (SANs: `localhost`, `127.0.0.1`, `::1`)
- `localhost+2-key.pem` - Private key

**Use Case**: Local HTTPS for `scripts/dev/start.sh` (Vite dev server and Go dev TLS).

**Note**: This is not mTLS material. gRPC mTLS certificates (CA/server/client) are minted by `scripts/dev/mint-accelerator-ca.sh` and `scripts/dev/mint-accelerator-cert.sh`.

---

### `install-nvidia-toolkit.sh`

**Purpose**: Install NVIDIA Container Toolkit on Docker hosts.

**Usage**:
```bash
./scripts/docker/install-nvidia-toolkit.sh
```

**Behavior**:
- Adds NVIDIA package repositories
- Installs `nvidia-container-toolkit`
- Restarts Docker daemon
- Enables GPU access in containers

**Use Case**: Setting up Docker hosts for GPU-enabled containers.

---

### `validate-env.sh`

**Purpose**: Validate environment configuration before deployment.

**Usage**:
```bash
./scripts/docker/validate-env.sh
```

**Checks**:
- SSL certificate files (`.secrets/localhost+2.pem` and its key)
- Docker installation
- Docker daemon availability
- NVIDIA Container Toolkit (`docker run --gpus all` smoke test)
- GPU availability (`nvidia-smi`)

**Use Case**: Pre-deployment validation in CI/CD pipelines.

---

## Build Stage Dependencies

### C++ Accelerator (ARM64/x86)
```
cuda-runtime (nvidia/cuda:runtime)
    │
    └─→ cpp-accelerator ───→ cpp-accelerator:X.Y.Z-amd64

bazel-base (ubuntu:24.04)
    │
    ├─→ cpp-dependencies ──→ cpp-builder ─────────────────┘
    │                                  │
proto-generated ───────────────────────┘
    │
proto-tools (buf)
```

### App (Go API)
```
runtime-base (ubuntu:24.04)
    │
proto-tools ──→ go-builder ──→ proto ──→ golang ──→ app
```

### Web Frontend
```
proto-tools ──→ proto ──→ web-frontend
```

### YOLO Model
```
ubuntu:24.04
    │
    └─→ yolo-tools ──→ yolo-model
```

---

## Integration with CI/CD

### ARM CI (`.github/workflows/docker-monorepo-build-arm.yml`)
```yaml
- job: detect-changes
  └─→ outputs: BUILD_CUDA_RUNTIME, BUILD_CPP_DEPS, etc.

- job: arm_pr / build_and_push
  └─→ scripts/ci/arm-build.sh
          ├─→ build-local.sh (build)
          └─→ pull-ghcr-cpp-intermediates.sh (cache)

- job: deploy_prod
  └─→ SSH to Jetson, docker compose pull/up
```

### x86 CI (`.github/workflows/docker-monorepo-build-x86.yml`)
```yaml
- job: push_app / push_web_frontend
  └─→ build-local.sh (build)
  └─→ push-tagged-images.sh (push)

- job: deploy_prod
  └─→ SSH to Cloud VM, docker compose pull/up
```

---

## Image Tagging Convention

All images follow this pattern:
```bash
<registry>/<namespace>/<image>:<version>-<arch>
<registry>/<namespace>/<image>:latest-<arch>
```

**Examples**:
```bash
ghcr.io/josnelihurt-code/learning-cuda/app:3.4.3-amd64
ghcr.io/josnelihurt-code/learning-cuda/app:latest-amd64
ghcr.io/josnelihurt-code/learning-cuda/cpp-accelerator:cpp-accelerator-1.2.3-proto0.9.0-arm64
ghcr.io/josnelihurt-code/learning-cuda/base:cuda-runtime-3.0.0-arm64
```

---

## Bazel Remote Cache

The `build-local.sh` script auto-detects bazel-remote cache servers on the LAN:

**Default Candidates**: `192.168.10.80:9092`, `192.168.30.60:9092`

**Override**:
```bash
BAZEL_REMOTE_CACHE=grpc://custom-host:9092 ./scripts/docker/build-local.sh
```

**Behavior**:
- Tries each candidate until one responds
- Sets `BAZEL_REMOTE_CACHE` and `BAZEL_REMOTE_UPLOAD_LOCAL_RESULTS`
- Proceeds without cache if none found (logs warning)

---

## Cross-Architecture Builds

The scripts support building for different architectures:

**Host Architecture Detection**:
```bash
uname -m  → x86_64 (amd64) or aarch64 (arm64)
```

**Cross-Build Warning**:
```bash
Warning: building for arm64 on host x86_64 without buildx may fail.
```

**Recommendation**: Use native runners for each architecture in CI.
