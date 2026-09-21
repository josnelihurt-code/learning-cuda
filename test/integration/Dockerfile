ARG UBUNTU_VERSION=24.04

FROM ubuntu:${UBUNTU_VERSION} AS integration-tests-base

ARG TARGETARCH=amd64
ARG GO_RELEASE

ENV DEBIAN_FRONTEND=noninteractive

WORKDIR /workspace

RUN apt-get update && apt-get install -y \
    curl \
    wget \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && GO_ARCH=$([ "$TARGETARCH" = "amd64" ] && echo "linux-amd64" || echo "linux-arm64") && \
    echo "Downloading Go for architecture: $GO_ARCH (TARGETARCH=$TARGETARCH)" && \
    wget https://go.dev/dl/go${GO_RELEASE}.${GO_ARCH}.tar.gz \
    && tar -C /usr/local -xzf go${GO_RELEASE}.${GO_ARCH}.tar.gz \
    && rm go${GO_RELEASE}.${GO_ARCH}.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/go"
ENV GO111MODULE=on

