ARG GO_VERSION

FROM golang:${GO_VERSION}-alpine AS go-builder-base

RUN apk add --no-cache git make

WORKDIR /build

ENV CGO_ENABLED=0
ENV GOOS=linux


