ARG ROOT
ARG BUILDINFO

# Build the manager binary
FROM golang:1.24.6 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.* .
COPY api/go.* api/
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN \
  --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  go mod download

# Copy the go source
COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/
ARG ROOT
ARG BUILDINFO
ARG FIPS_VERSION
ARG GODEBUG_ARGS
# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
ENV GOCACHE=/root/.cache/go-build
RUN --mount=type=cache,target="/root/.cache/go-build"
RUN \
  --mount=type=cache,target=/root/.cache \
  --mount=type=cache,target=/go/pkg/mod \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} GODEBUG=${GODEBUG_ARGS} GOFIPS140=${FIPS_VERSION} go build -a \
   -ldflags="-X 'github.com/VictoriaMetrics/VictoriaMetrics/lib/buildinfo.Version=operator-${BUILDINFO}'" \
   -o app ${ROOT}/

# Use scratch as minimal base image to package the manager binary
FROM scratch
WORKDIR /
COPY --from=builder /workspace/app .
USER 65532:65532

ENTRYPOINT ["/app"]
