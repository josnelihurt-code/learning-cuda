ARG CUDA_VERSION=12.5.1
ARG UBUNTU_VERSION=24.04

FROM nvidia/cuda:${CUDA_VERSION}-devel-ubuntu${UBUNTU_VERSION} AS bazel-base

ARG TARGETARCH=amd64
ARG BAZEL_VERSION=8.4.2
ARG BAZELISK_VERSION=v1.19.0

ENV DEBIAN_FRONTEND=noninteractive
ENV USE_BAZEL_VERSION=${BAZEL_VERSION}

WORKDIR /build

RUN apt-get update && apt-get install -y \
    wget \
    git \
    build-essential \
    pkg-config \
    zip \
    unzip \
    python3 \
    python3-pip \
    ffmpeg \
    libavcodec-dev \
    libavutil-dev \
    libswscale-dev \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# TensorRT for YOLO inference.
# Pinned to the exact version the cuda-runtime base image ships (libnvinfer10
# 10.16.1.11, soname libnvinfer.so.10). Unpinned, apt follows the NVIDIA CUDA
# repo's latest, which drifted to TRT 11 (soname .so.11) — binaries then fail
# to load in the Jetson runtime image. Keep in sync with
# src/cpp_accelerator/docker-cuda-runtime/Dockerfile.
ARG TENSORRT_VERSION=10.16.1.11-1+cuda13.2

RUN apt-get update && \
    TRT_PKGS="libnvinfer-dev=${TENSORRT_VERSION} libnvonnxparsers-dev=${TENSORRT_VERSION}" && \
    if [ "$TARGETARCH" = "amd64" ]; then \
        apt-get install -y --no-install-recommends libnvinfer-bin=${TENSORRT_VERSION} ${TRT_PKGS} && \
        apt-get clean && \
        rm -rf /var/lib/apt/lists/*; \
    elif [ "$TARGETARCH" = "arm64" ]; then \
        echo "Installing TensorRT ${TENSORRT_VERSION} dev libraries for ARM64 (compilation only)..."; \
        apt-get install -y --no-install-recommends ${TRT_PKGS} && \
        apt-get clean && \
        rm -rf /var/lib/apt/lists/*; \
    fi

RUN BAZEL_ARCH=$([ "$TARGETARCH" = "amd64" ] && echo "linux-amd64" || echo "linux-arm64") && \
    echo "Downloading Bazelisk ${BAZELISK_VERSION} for architecture: $BAZEL_ARCH (TARGETARCH=$TARGETARCH)" && \
    wget -O /usr/local/bin/bazel https://github.com/bazelbuild/bazelisk/releases/download/${BAZELISK_VERSION}/bazelisk-${BAZEL_ARCH} \
    && chmod +x /usr/local/bin/bazel

