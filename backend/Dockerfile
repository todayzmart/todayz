# syntax=docker/dockerfile:1
#
# TodayZ backend image (W1-10): one static binary, `todayz`, for the api, the worker and the
# one-off migrate task. Multi-stage; the final stage is distroless, non-root, with no shell.
#
# Build context is backend/ and needs the generated code (backend/gen is git-ignored), so run
# `make gen` first:
#   make gen && docker build -t todayz-backend backend
#   docker buildx build --platform linux/arm64 ...   # Graviton (t4g) in Wave 5
#
# Run (the command is the argument; the default is the api):
#   docker run --rm -e APP_ENV=... todayz-backend api|worker|migrate up|version
# Deployed ports: api :8080 (API_ADDR), worker health :8081 (WORKER_ADDR). The image sets no
# secret and no default for any of them; Beanstalk injects the environment (09 §2).

ARG GO_VERSION=1.26
ARG DISTROLESS=gcr.io/distroless/static-debian12:nonroot

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-mod=readonly
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags "-s -w \
        -X github.com/todayz/backend/internal/platform/buildinfo.Version=${VERSION} \
        -X github.com/todayz/backend/internal/platform/buildinfo.Commit=${COMMIT} \
        -X github.com/todayz/backend/internal/platform/buildinfo.BuildTime=${BUILD_TIME} \
        -X github.com/todayz/backend/internal/platform/buildinfo.Modified=false" \
      -o /out/todayz ./cmd/todayz

FROM ${DISTROLESS}
# distroless "nonroot" is uid/gid 65532; naming it keeps the intent checkable.
USER 65532:65532
COPY --from=build --chown=65532:65532 /out/todayz /todayz
EXPOSE 8080 8081
ENTRYPOINT ["/todayz"]
CMD ["api"]
