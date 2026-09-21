# Unit test image for the Go API.
#
# Runs the go_api suite (build + vet + test -race) inside the same
# containerized Go environment the build pipeline uses, so CI results never
# depend on whatever toolchain happens to be installed on the runner.
#
# The stubs stage regenerates the Go protobuf code because proto/gen is not
# committed to the repository; it chains after the bufgen image. Build with:
#
#   docker build -t cuda-learning-bufgen:ci -f bufgen.dockerfile .
#   docker build -t cuda-learning-go-test:ci -f test/unit/Dockerfile .
#   docker run --rm cuda-learning-go-test:ci

ARG BUFGEN_IMAGE=cuda-learning-bufgen:ci
ARG GO_VERSION=1.27

FROM ${BUFGEN_IMAGE} AS proto-stubs

WORKDIR /stubs
COPY buf.gen.backend.yaml .
COPY proto ./proto
RUN buf generate --template buf.gen.backend.yaml

FROM golang:${GO_VERSION}-alpine

# git for module fetches, build-base for CGO — same toolset as the
# lint-golang service in docker-compose.dev.yml.
RUN apk add --no-cache git build-base

WORKDIR /workspace
ENV CGO_ENABLED=1
ENV GOFLAGS=-buildvcs=false

# Modules in their own layer so warm docker caches skip the download.
COPY go.mod go.sum ./
RUN go mod download

COPY src/go_api ./src/go_api
COPY --from=proto-stubs /stubs/proto/gen ./proto/gen

# The loader package needs the CUDA toolchain and is excluded, matching
# scripts/test/unit-tests.sh.
CMD ["/bin/sh", "-c", "go build ./src/go_api/... && go vet ./src/go_api/... && go test -race $(go list ./src/go_api/... | grep -v 'src/go_api/pkg/infrastructure/processor/loader')"]
