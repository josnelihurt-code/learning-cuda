ARG GO_VERSION=1.27

FROM golang:${GO_VERSION}-alpine AS go-builder-base

RUN apk add --no-cache git make

WORKDIR /build

ENV CGO_ENABLED=0
ENV GOOS=linux


