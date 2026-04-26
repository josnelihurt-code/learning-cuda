#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

CUDA_LEARNING_RUNTIME_DIR="${CUDA_LEARNING_RUNTIME_DIR:-/tmp/cuda-learning}"
DEV_PID_DIR="${CUDA_LEARNING_RUNTIME_DIR}/PIDs"
DEV_LOG_DIR="${CUDA_LEARNING_RUNTIME_DIR}/logs"

DEV_PID_GRPC="${DEV_PID_DIR}/grpc.pid"
DEV_PID_GO="${DEV_PID_DIR}/go.pid"
DEV_PID_VITE="${DEV_PID_DIR}/vite.pid"

DEV_LOG_GRPC="${DEV_LOG_DIR}/accelerator-client.log"
DEV_LOG_ACCELERATOR="/tmp/cppaccelerator.log"
DEV_LOG_GO="${DEV_LOG_DIR}/goserver.log"
DEV_LOG_VITE="${DEV_LOG_DIR}/vite.log"

cd "$PROJECT_ROOT"

BUILD_FIRST=false
SHOW_HELP=false
GRPC_PID=
GO_PID=
VITE_PID=

die() {
    echo "Error: $*" >&2
    exit 1
}

# Updates .vscode/launch.json "processId" for the Go attach configuration.
# On missing python3 or launch file, no-op.
update_launch_go_attach_process_id() {
    local pid="$1"
    local launch_json="${PROJECT_ROOT}/.vscode/launch.json"

    command -v python3 >/dev/null 2>&1 || return 0
    [ -f "$launch_json" ] || return 0

    if ! python3 "$SCRIPT_DIR/update_launch_go_attach_process_id.py" "$launch_json" "$pid"
    then
        echo "Warning: could not update Go attach processId in .vscode/launch.json" >&2
    fi
}

# Updates .vscode/launch.json "processId" for the C++ accelerator attach
# configuration. On missing python3 or launch file, no-op.
update_launch_cpp_attach_process_id() {
    local pid="$1"
    local launch_json="${PROJECT_ROOT}/.vscode/launch.json"

    command -v python3 >/dev/null 2>&1 || return 0
    [ -f "$launch_json" ] || return 0

    if ! python3 "$SCRIPT_DIR/update_launch_cpp_attach_process_id.py" "$launch_json" "$pid"
    then
        echo "Warning: could not update C++ attach processId in .vscode/launch.json" >&2
    fi
}

parse_args() {
    for arg in "$@"; do
        case "$arg" in
            --build|-b)
                BUILD_FIRST=true
                ;;
            --help|-h)
                SHOW_HELP=true
                ;;
        esac
    done
}

print_help() {
    echo "Usage: ./scripts/dev/start.sh [OPTIONS]"
    echo ""
    echo "Starts the C++ accelerator client, Go API (HTTPS), and Vite dev server."
    echo ""
    echo "Local dev (split ports):"
    echo "  - UI:  https://localhost:3000/react and https://localhost:3000/lit (Vite pretty paths)"
    echo "  - API: https://localhost:8443 — REST, WebSocket, Connect (Vite proxies to Go via VITE_API_ORIGIN)"
    echo ""
    echo "Production: Traefik routes to Nginx (web-frontend) for static HTML; the Go app serves /api, WebRTC signaling, etc."
    echo "Nginx serves /react and /lit; Go does not serve frontend HTML in production."
    echo ""
    echo "VITE_API_ORIGIN defaults to https://localhost:8443 when unset."
    echo "Vite only (Go already running): cd src/front-end && npm run dev (needs .secrets/localhost+2*.pem)."
    echo ""
    echo "Options:"
    echo "  --build, -b    Build C++ accelerator client (TensorRT) and Go backend before starting"
    echo "  --help, -h     Show this help message"
    echo ""
    echo "Examples:"
    echo "  ./scripts/dev/start.sh         # Full stack (uses existing binary)"
    echo "  ./scripts/dev/start.sh --build # Build with TensorRT + full stack"
    exit 0
}

require_secrets() {
    [ -f ".secrets/development.env" ] || {
        echo "Error: .secrets/development.env not found." >&2
        echo "      Copy .secrets/development.env.example to .secrets/development.env and fill in values." >&2
        exit 1
    }
    echo "Loading development secrets from .secrets/development.env"
    source .secrets/development.env
}

ensure_tls_certs() {
    if [ ! -f ".secrets/localhost+2.pem" ]; then
        echo "SSL certificates not found, generating..."
        ./scripts/docker/generate-certs.sh
        echo ""
    fi
}

ensure_state_dirs() {
    mkdir -p "$DEV_PID_DIR" "$DEV_LOG_DIR"
}

truncate_dev_logs() {
    echo "Truncating dev logs..."
    for f in "$DEV_LOG_GRPC" "$DEV_LOG_GO" "$DEV_LOG_VITE"; do
        : >"$f"
    done
}

run_optional_build() {
    if [ "$BUILD_FIRST" != true ]; then
        return 0
    fi
    echo "Checking proto files..."
    if [ ! -f "proto/gen/image_processor_service.pb.go" ]; then
        ./scripts/build/protos.sh
    fi

    echo "Building C++ accelerator client..."
    bazel build //src/cpp_accelerator/ports/grpc:accelerator_control_client

    echo "Building backend with Go..."
    (cd src/go_api && make build)
}

require_grpc_binary() {
    GRPC_SERVER_BIN="${PROJECT_ROOT}/bazel-bin/src/cpp_accelerator/ports/grpc/accelerator_control_client"
    if [ ! -x "$GRPC_SERVER_BIN" ]; then
        echo "Error: accelerator client binary not found at ${GRPC_SERVER_BIN}" >&2
        echo "       Run './scripts/dev/start.sh --build' to build it." >&2
        exit 1
    fi
}

start_grpc() {
    GRPC_SERVER_BIN="${PROJECT_ROOT}/bazel-bin/src/cpp_accelerator/ports/grpc/accelerator_control_client"
    echo "Starting C++ accelerator client..."
    "$GRPC_SERVER_BIN" \
        --control_addr=localhost:60062 \
        --client_cert="${PROJECT_ROOT}/.secrets/dev-accelerator-client.pem" \
        --client_key="${PROJECT_ROOT}/.secrets/dev-accelerator-client-key.pem" \
        --ca_cert="${PROJECT_ROOT}/.secrets/accelerator-ca.pem" \
        >"$DEV_LOG_GRPC" 2>&1 &
    GRPC_PID=$!
    echo "$GRPC_PID" >"$DEV_PID_GRPC"

    if ! kill -0 "$GRPC_PID" 2>/dev/null; then
        update_launch_cpp_attach_process_id -1
        die "C++ accelerator client failed to start"
    fi

    update_launch_cpp_attach_process_id "$GRPC_PID"
}

start_go() {
    echo "Starting Go server..."
    echo "Building Go server..."
    if ! (cd "$PROJECT_ROOT/src/go_api" && make build); then
        update_launch_go_attach_process_id -1
        die "Go build failed"
    fi

    if [ ! -f "$PROJECT_ROOT/bin/server" ]; then
        update_launch_go_attach_process_id -1
        die "Go build did not produce bin/server"
    fi

    "$PROJECT_ROOT/bin/server" -config=config/config.dev.yaml >"$DEV_LOG_GO" 2>&1 &
    GO_PID=$!
    echo "$GO_PID" >"$DEV_PID_GO"

    if ! kill -0 "$GO_PID" 2>/dev/null; then
        update_launch_go_attach_process_id -1
        die "Go server failed to start"
    fi

    update_launch_go_attach_process_id "$GO_PID"
}

start_vite() {
    cd "$PROJECT_ROOT/src/front-end"
    [ ! -d "node_modules" ] && npm install

    echo "Dev: UI  https://localhost:3000 | API https://localhost:8443 (VITE_API_ORIGIN) | Prod UI: Nginx, not Go"
    echo "Starting Vite at https://localhost:3000 (API proxy -> ${VITE_API_ORIGIN:-https://localhost:8443})"
    npm run dev >"$DEV_LOG_VITE" 2>&1 &
    VITE_PID=$!
    echo "$VITE_PID" >"$DEV_PID_VITE"
    cd "$PROJECT_ROOT"
}

cleanup_on_signal() {
    echo "Stopping services..."
    [ -n "${VITE_PID:-}" ] && kill "$VITE_PID" 2>/dev/null || true
    [ -n "${GO_PID:-}" ] && kill "$GO_PID" 2>/dev/null || true
    [ -n "${GRPC_PID:-}" ] && kill "$GRPC_PID" 2>/dev/null || true
    wait ${VITE_PID:-} ${GO_PID:-} ${GRPC_PID:-} 2>/dev/null || true
    rm -f "$DEV_PID_GRPC" "$DEV_PID_GO" "$DEV_PID_VITE"
}

get_local_ip() {
    local local_ip

    local_ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
    if [ -z "$local_ip" ]; then
        local_ip="$(ip route get 1.1.1.1 2>/dev/null | awk '{for (i=1;i<=NF;i++) if ($i=="src") {print $(i+1); exit}}')"
    fi
    [ -z "$local_ip" ] && local_ip="localhost"

    echo "$local_ip"
}

print_summary() {
    local local_ip
    local_ip="$(get_local_ip)"

    echo "================================================"
    echo "Dev stack:"
    echo "  UI (Vite):   https://${local_ip}:3000"
    echo "  API (HTTPS): https://${local_ip}:8443"
    echo "  Accelerator: → ${local_ip}:60062 (outbound)"
    echo "================================================"
    echo ""
    echo "  Accelerator PID: $GRPC_PID ($DEV_PID_GRPC)"
    echo "  Go server PID:   $GO_PID ($DEV_PID_GO)"
    echo "  Vite PID:        $VITE_PID ($DEV_PID_VITE)"
    echo ""
    echo "To stop: ./scripts/dev/stop.sh"
    echo "Logs:"
    echo "  tail -f $DEV_LOG_GRPC"
    echo "  tail -f $DEV_LOG_ACCELERATOR"
    echo "  tail -f $DEV_LOG_GO"
    echo "  tail -f $DEV_LOG_VITE"
}

main() {
    parse_args "$@"
    [ "$SHOW_HELP" = true ] && print_help

    require_secrets
    ensure_tls_certs

    echo "Stopping previous dev processes..."
    ./scripts/dev/stop.sh 2>/dev/null || true

    run_optional_build
    require_grpc_binary
    ensure_state_dirs
    truncate_dev_logs

    trap cleanup_on_signal INT TERM

    start_go
    start_grpc
    start_vite

    print_summary
}

main "$@"
