# Build the manager binary
# Override BASE_IMAGE to build from another registry. Pin it by digest.
ARG BASE_IMAGE=golang:1.27.4@sha256:5f1b98fa02fe396ebce2a0227a0e08309e11792915b9dd2e1f71053d7e7e6666

# Build the web UI. The pnpm settings block install scripts and new releases. The build stages run on the platform of
# the build machine and cross-compile, so a multi-platform build needs no emulation.
FROM --platform=$BUILDPLATFORM node:26.13.1-alpine@sha256:6bd9ab43c0ec0190ca7b2d193906cb8b7edf6e7f4afa43ddeccd6452710bffb5 AS ui
WORKDIR /ui
RUN npm install --global --ignore-scripts pnpm@12.6.0
COPY ui/package.json ui/pnpm-lock.yaml ui/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY ui/ ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM ${BASE_IMAGE} AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the Go source (relies on .dockerignore to filter)
COPY . .
COPY --from=ui /ui/dist ui/dist

# Build
# the GOARCH has no default value to allow the binary to be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
# -s -w removes the symbol table and DWARF data. This halves the binaries. Panics still show stack traces.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o manager cmd/main.go && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o chronicle-exec ./cmd/chronicle-exec && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -tags ui -o chronicle-server ./cmd/chronicle-server

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5
WORKDIR /
COPY --from=builder /workspace/manager .
# Workflow Pods run chronicle-exec from an image volume of this image. With executorImageVolume false, an init
# container copies it.
COPY --from=builder /workspace/chronicle-exec /chronicle-exec
COPY --from=builder /workspace/chronicle-server /chronicle-server
USER 65532:65532

ENTRYPOINT ["/manager"]
