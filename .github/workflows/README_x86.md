# x86 CI/CD Workflow

This document explains the x86_64 Docker build and deployment pipeline for the CUDA Learning Platform.

## Overview

The x86 workflow builds and deploys the `app` (Go API) and `web-frontend` images to a cloud VM. It implements smart CI with independent change detection for each component.

## File: `docker-monorepo-build-x86.yml`

### Triggers

| Event | Condition | Behavior |
|-------|-----------|----------|
| `push` | `main` branch | Build and push images |
| `pull_request` | `main` branch | Build only (no push) |
| `workflow_dispatch` | Manual + optional `force_all` | Full rebuild |

## Jobs

### 1. `detect-changes`

**Purpose**: Determine which components changed and what needs rebuilding.

**Outputs**:
| Output | Description |
|--------|-------------|
| `app` | Go API image needs building |
| `web_frontend` | Frontend image needs building |
| `yolo_model` | YOLO model needs generating |
| `app_version_changed` | `src/go_api/VERSION` changed |
| `web_frontend_version_changed` | `src/front-end/VERSION` changed |
| `deployable` | Either VERSION changed (triggers deploy) |

**Path Filters**:
```yaml
app:
  - proto/**
  - src/go_api/**
  - Dockerfile
  - runtime/**
  - data/**
  - buf.yaml, buf.lock, buf.gen.yaml, buf.gen.backend.yaml

app_version:
  - src/go_api/VERSION

web_frontend:
  - proto/**
  - src/front-end/**
  - buf.yaml, buf.lock, buf.gen.yaml
  - scripts/docker/build-local.sh
  - scripts/docker/push-tagged-images.sh

web_frontend_version:
  - src/front-end/VERSION

yolo_model:
  - src/cpp_accelerator/yolo-model-gen/Dockerfile
  - src/cpp_accelerator/yolo-model-gen/VERSION
  - scripts/models/export_yolo_to_onnx.py
```

**Decision Logic**:
- `workflow_dispatch`: forces all builds and deployment
- `force_all` input: forces all builds
- `ci:full-build` PR label: forces all builds
- Otherwise: build only what changed

---

### 2. `build_app` (PR only)

**Purpose**: Build app image locally for PR validation.

**Conditions**: `github.event_name == 'pull_request' && app == 'true'`

**Stages Built**:
```
proto-tools → go-builder → runtime-base → proto → golang → app
```

**Registry**: `local` (no push)

---

### 3. `build_web_frontend` (PR only)

**Purpose**: Build web-frontend image locally for PR validation.

**Conditions**: `github.event_name == 'pull_request' && web_frontend == 'true'`

**Stages Built**:
```
proto-tools → proto → web-frontend
```

**Registry**: `local` (no push)

---

### 4. `build_yolo_model` (PR only)

**Purpose**: Build YOLO model artifact for PR validation.

**Conditions**: `github.event_name == 'pull_request' && yolo_model == 'true'`

**Stages Built**:
```
yolo-tools → yolo-model
```

**Registry**: `local` (no push)

---

### 5. `push_app`

**Purpose**: Build and push app image to GHCR.

**Conditions**: `(push || workflow_dispatch) && app == 'true'`

**Stages Built**:
```
proto-tools → go-builder → runtime-base → proto → golang → app
```

**Registry**: `ghcr.io/josnelihurt-code/learning-cuda`

**Script**: `scripts/docker/build-local.sh` + `scripts/docker/push-tagged-images.sh`

**Environment**:
```bash
LATEST_ALIASES=app
ARCH=amd64
```

---

### 6. `push_web_frontend`

**Purpose**: Build and push web-frontend image to GHCR.

**Conditions**: `(push || workflow_dispatch) && web_frontend == 'true'`

**Stages Built**:
```
proto-tools → proto → web-frontend
```

**Registry**: `ghcr.io/josnelihurt-code/learning-cuda`

**Script**: `scripts/docker/build-local.sh` + `scripts/docker/push-tagged-images.sh`

**Environment**:
```bash
LATEST_ALIASES=web-frontend
ARCH=amd64
```

---

### 7. `push_yolo_model`

**Purpose**: Build and push YOLO model artifact to GHCR.

**Conditions**: `(push || workflow_dispatch) && yolo_model == 'true'`

**Stages Built**:
```
yolo-tools → yolo-model
```

**Registry**: `ghcr.io/josnelihurt-code/learning-cuda`

**Script**: `scripts/docker/build-local.sh` + `scripts/docker/push-tagged-images.sh`

**Environment**:
```bash
LATEST_ALIASES=yolo-model-gen
ARCH=amd64
```

---

### 8. `deploy_prod`

**Purpose**: Deploy app and web-frontend to Cloud VM.

**Conditions**:
```yaml
((push && main) || workflow_dispatch)
&& deployable == 'true'
```

**Key Behavior**: Only deploys when `src/go_api/VERSION` **OR** `src/front-end/VERSION` is bumped.

**Deployment Steps**:
1. Validate Cloud VM SSH keys and secrets
2. Checkout repository
3. Configure SSH key
4. Add Cloud VM to known_hosts
5. Sync `learning-cuda.yaml` compose file
6. Sync production config files
7. Sync data files
8. Sync mTLS certificates to VM
9. Pull new images via `docker compose`
10. Restart `cuda-go-server` and `cuda-web-frontend`

**Environment Variables** (from GitHub Secrets or hardcoded workflow env):
| Variable | Source | Description |
|----------|--------|-------------|
| `CLOUD_VM_HOST` | Secret | Cloud VM hostname/IP |
| `CLOUD_VM_USER` | Secret | SSH user |
| `CLOUD_VM_SSH_KEY` | Secret | SSH private key |
| `CLOUD_VM_SSH_PORT` | Workflow env | SSH port (hardcoded default `22`) |
| `COMPOSE_DIRECTORY` | Workflow env | Compose files location (hardcoded default) |
| `COMPOSE_WORKDIR` | Workflow env | Docker compose working directory (hardcoded default) |
| `ACCELERATOR_SERVER_CERT` | Secret | mTLS server certificate |
| `ACCELERATOR_SERVER_KEY` | Secret | mTLS server key |
| `ACCELERATOR_CA_CERT` | Secret | mTLS CA certificate |

**Images Used** (versioned tags computed at runtime from VERSION files):
```bash
APP_IMAGE=ghcr.io/josnelihurt-code/learning-cuda/app:${go_version}-amd64
CUDA_WEB_FRONTEND_IMAGE=ghcr.io/josnelihurt-code/learning-cuda/web-frontend:fe-${fe_version}-proto${proto_version}-amd64
```

---

## Image Build Chains

### App (Go API)
```
golang:${GO_VERSION}-alpine
    │
    ├─→ proto-tools ──→ proto ────┐
    │                              │
    └─→ go-builder ───→ golang ────┤
                                   │
alpine:${ALPINE_VERSION}           │
    │                              │
    └─→ runtime-base ───→ app ─────┴──→ :latest-amd64
```

### Web Frontend
```
golang:${GO_VERSION}-alpine
    │
    └─→ proto-tools ───→ proto ───→ web-frontend ───→ :latest-amd64
```

### YOLO Model
```
python:${PYTHON_VERSION}-slim
    │
    └─→ yolo-tools ───→ yolo-model ───→ :latest-amd64
```

### Runtime Base (shared)
```
alpine:${ALPINE_VERSION} ───→ runtime-base ───→ :latest-amd64
```

---

## VERSION File Gating

| Component | VERSION File | Deploy Gate |
|-----------|--------------|-------------|
| app (Go API) | `src/go_api/VERSION` | Triggers deploy if changed |
| web-frontend | `src/front-end/VERSION` | Triggers deploy if changed |
| yolo-model | `src/cpp_accelerator/yolo-model-gen/VERSION` | No direct deploy gate |

**Deploy Logic**:
```bash
deployable = (app_version_changed == 'true') || (web_frontend_version_changed == 'true')
```

**Why gate on VERSION?**
- Prevents unnecessary cloud VM deployments
- Ensures production only gets versioned releases
- Allows config/docs changes without triggering deploys
- Independent gating: app OR frontend can trigger deploy

---

## Dependencies Between Files

```
docker-monorepo-build-x86.yml
    │
    ├─→ scripts/docker/build-local.sh
    │       │
    │       ├─→ proto/docker-build-base/Dockerfile (proto-tools)
    │       ├─→ src/go_api/builder/Dockerfile (go-builder)
    │       ├─→ runtime/Dockerfile (runtime-base)
    │       ├─→ proto/Dockerfile (proto)
    │       ├─→ src/go_api/Dockerfile.build (golang)
    │       ├─→ Dockerfile (app)
    │       ├─→ src/front-end/Dockerfile (web-frontend)
    │       └─→ src/cpp_accelerator/yolo-model-gen/Dockerfile (yolo)
    │
    └─→ scripts/docker/push-tagged-images.sh
```

---

## Architecture-Specific Details

### Cloud VM Target
- **Architecture**: x86_64 (amd64)
- **OS**: Ubuntu Linux
- **Role**: Production Go API and Frontend server

### Self-Hosted Runner
- **Runs on**: x86_64 machine
- **Tag**: `["self-hosted", "Linux", "X64"]`
- **Advantage**: Native compilation, faster builds

---

## Independent Component Building

The x86 workflow builds components independently:

| Scenario | app builds? | web-frontend builds? | yolo_model builds? | Deploy? |
|----------|-------------|---------------------|-------------------|---------|
| Only Go files change | ✅ | ❌ | ❌ | ✅ (if VERSION) |
| Only frontend files change | ❌ | ✅ | ❌ | ✅ (if VERSION) |
| Only YOLO files change | ❌ | ❌ | ✅ | ❌ |
| Proto files change | ✅ | ✅ | ❌ | Depends |
| Config files change | ❌ | ❌ | ❌ | ❌ |

This optimization reduces CI time and resource usage.
