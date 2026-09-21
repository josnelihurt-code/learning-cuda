# Unit test image for the Go API: runs build + vet + test -race in the same
# containerized Go environment as the build pipeline. The stubs stage
# regenerates the untracked proto/gen code via the bufgen image:
#
#   docker build -t cuda-learning-bufgen:ci -f bufgen.dockerfile .
#   docker build -t cuda-learning-go-test:ci -f test/unit/Dockerfile .

ARG BUFGEN_IMAGE=cuda-learning-bufgen:ci
ARG GO_VERSION

FROM ${BUFGEN_IMAGE} AS proto-stubs

WORKDIR /stubs
COPY buf.gen.backend.yaml .
COPY proto ./proto
RUN buf generate --template buf.gen.backend.yaml

FROM golang:${GO_VERSION}-alpine

RUN apk add --no-cache git build-base

WORKDIR /workspace
ENV CGO_ENABLED=1
ENV GOFLAGS=-buildvcs=false

COPY go.mod go.sum ./
RUN go mod download

COPY src/go_api ./src/go_api
COPY --from=proto-stubs /stubs/proto/gen ./proto/gen

CMD ["/bin/sh", "-c", "go build ./src/go_api/... && go vet ./src/go_api/... && go test -race $(go list ./src/go_api/... | grep -v 'src/go_api/pkg/infrastructure/processor/loader')"]
