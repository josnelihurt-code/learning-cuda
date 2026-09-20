ARG BASE_REGISTRY=ghcr.io/josnelihurt-code/learning-cuda
ARG BASE_TAG=latest
ARG TARGETARCH=amd64

# L4T Multimedia source for the ARM64 Argus-camera build.
# Pass --build-arg L4T_MULTIMEDIA_IMAGE=nvcr.io/nvidia/l4t-jetpack:r36.3.0 on arm64
# to copy the real libnvbuf_utils.so from NVIDIA's JetPack image.
# Default (alpine) carries no nvbuf libs; the RUN below falls back to a stub.
ARG L4T_MULTIMEDIA_IMAGE=alpine:3.21
FROM ${L4T_MULTIMEDIA_IMAGE} AS l4t-multimedia

FROM ${BASE_REGISTRY}/base:bazel-base-${BASE_TAG}-${TARGETARCH}

# Re-declare so it is available in RUN commands within this stage
# (global ARGs are only usable in FROM instructions without re-declaration).
ARG TARGETARCH=amd64

WORKDIR /build

RUN apt-get update && apt-get install -y --no-install-recommends \
    libegl-dev \
    libgstreamer1.0-dev \
    libgstreamer-plugins-base1.0-dev \
    && rm -rf /var/lib/apt/lists/*

# Provide libnvbuf_utils.so for the Argus-enabled Bazel build (arm64 only).
# When L4T_MULTIMEDIA_IMAGE points to a real JetPack image the .so is copied
# directly.  When the default alpine image is used (amd64, or arm64 without
# --build-arg L4T_MULTIMEDIA_IMAGE) a stub is compiled instead so the linker
# is satisfied without requiring JetPack on the build host.
# The production container mounts the real library from the Jetson host at
# runtime (see src/cpp_accelerator/docker-compose.yml).
RUN --mount=type=bind,from=l4t-multimedia,source=/,target=/l4t,ro \
    if ls /l4t/usr/lib/aarch64-linux-gnu/libnvbuf_utils.so* 2>/dev/null | grep -q .; then \
        cp -P /l4t/usr/lib/aarch64-linux-gnu/libnvbuf_utils.so* /usr/lib/aarch64-linux-gnu/ && \
        echo "libnvbuf_utils.so: installed from L4T image"; \
    elif [ "$TARGETARCH" = "arm64" ]; then \
        printf 'int NvBufSurfaceMap(void*s,int a,int b,int c){(void)s;(void)a;(void)b;(void)c;return -1;}\nint NvBufSurfaceUnMap(void*s,int a,int b){(void)s;(void)a;(void)b;return -1;}\nint NvBufSurfaceSyncForDevice(void*s,int a,int b){(void)s;(void)a;(void)b;return -1;}\nint NvBufSurfaceSyncForCpu(void*s,int a,int b){(void)s;(void)a;(void)b;return -1;}\nint NvBufSurfaceMapEglImage(void*s,int a){(void)s;(void)a;return -1;}\nint NvBufSurfaceUnMapEglImage(void*s,int a){(void)s;(void)a;return -1;}\n' > /tmp/nvbuf_stub.c && \
        gcc -shared -fPIC -o /usr/lib/aarch64-linux-gnu/libnvbuf_utils.so /tmp/nvbuf_stub.c && \
        echo "libnvbuf_utils.so: linker stub compiled (pass --build-arg L4T_MULTIMEDIA_IMAGE=nvcr.io/nvidia/l4t-jetpack:r36.3.0 to use the real library)"; \
    else \
        echo "libnvbuf_utils.so: not needed on amd64"; \
    fi

COPY MODULE.bazel MODULE.bazel.lock WORKSPACE.bazel BUILD.bazel ./
COPY .bazelrc .bazeliskrc ./
COPY bazel/ ./bazel/
COPY proto/ ./proto/
COPY third_party/ ./third_party/
COPY buf.yaml buf.lock buf.gen.yaml ./

ENV USE_BAZEL_VERSION=8.4.2
RUN bazel --batch fetch //...

RUN bazel --batch build \
    --show_timestamps \
    --announce_rc \
    @spdlog//:spdlog \
    @com_google_protobuf//:protobuf \
    //proto:common_cc_proto \
    //proto:image_processor_service_cc_proto \
    //proto:config_service_cc_proto \
    //third_party/stb:stb_image \
    //third_party/stb:stb_image_write
