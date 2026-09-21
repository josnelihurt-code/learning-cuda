ARG BASE_REGISTRY=ghcr.io/josnelihurt-code/learning-cuda
ARG BASE_TAG=latest
ARG TARGETARCH=amd64
ARG ALPINE_VERSION

FROM ${BASE_REGISTRY}/base:proto-tools-${BASE_TAG}-${TARGETARCH} AS proto-gen-builder

FROM alpine:${ALPINE_VERSION} AS proto-generated

RUN apk add --update --no-cache \
    ca-certificates \
    git \
    protoc \
    protobuf-dev \
    && rm -rf /var/cache/apk/*

COPY --from=proto-gen-builder /go/bin/buf /usr/local/bin/buf
COPY --from=proto-gen-builder /go/bin/protoc-gen-go /usr/local/bin/protoc-gen-go
COPY --from=proto-gen-builder /go/bin/protoc-gen-connect-go /usr/local/bin/protoc-gen-connect-go
COPY --from=proto-gen-builder /go/bin/protoc-gen-go-grpc /usr/local/bin/protoc-gen-go-grpc

WORKDIR /workspace
ENV XDG_CACHE_HOME=/workspace/.cache

COPY buf.yaml buf.lock buf.gen.backend.yaml ./
COPY proto/*.proto ./proto/

RUN buf generate --template buf.gen.backend.yaml
