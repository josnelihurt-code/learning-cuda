ARG GO_VERSION

FROM golang:${GO_VERSION}-alpine AS proto-tools

ARG BUF_VERSION=v1.47.2
ARG PROTOC_GEN_GO_VERSION=v1.35.2
ARG PROTOC_GEN_CONNECT_GO_VERSION=v1.19.1
ARG PROTOC_GEN_GO_GRPC_VERSION=v1.5.1

RUN go install github.com/bufbuild/buf/cmd/buf@${BUF_VERSION} && \
    go install connectrpc.com/connect/cmd/protoc-gen-connect-go@${PROTOC_GEN_CONNECT_GO_VERSION} && \
    go install google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOC_GEN_GO_VERSION} && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@${PROTOC_GEN_GO_GRPC_VERSION}

WORKDIR /tools

ENTRYPOINT ["/go/bin/buf"]

