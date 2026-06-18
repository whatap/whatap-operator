#!/usr/bin/env bash
set -euo pipefail

# Display usage information
function show_usage {
  echo "❗ 사용법: ./build.sh <VERSION> [<ARCH>] [<REGISTRY>]"
  echo "  <VERSION>: 빌드할 버전 (예: 1.7.15)"
  echo "  <ARCH>: 빌드할 아키텍처 (옵션: amd64, arm64, all) [기본값: all]"
  echo "  <REGISTRY>: 사용할 레지스트리 (기본값: public.ecr.aws/whatap)"
  echo "예: ./build.sh 1.7.15 arm64"
  echo "    ./build.sh 1.7.15 all docker.io/myuser"
}

# Check if at least one argument is provided
if [ $# -lt 1 ]; then
  show_usage
  exit 1
fi

VERSION=$1
ARCH=${2:-all}  # Default to 'all' if not specified
REGISTRY=${3:-public.ecr.aws/whatap}  # Default registry

# Set the platforms based on the architecture parameter
case $ARCH in
  amd64)
    PLATFORMS="linux/amd64"
    ARCH_MSG="amd64"
    ;;
  arm64)
    PLATFORMS="linux/arm64"
    ARCH_MSG="arm64"
    ;;
  all)
    PLATFORMS="linux/arm64,linux/amd64"
    ARCH_MSG="all architectures (linux/arm64, linux/amd64)"
    ;;
  *)
    echo "❗ 지원하지 않는 아키텍처입니다: $ARCH"
    show_usage
    exit 1
    ;;
esac

# Set image names with the specified registry
export IMG="${REGISTRY}/whatap-operator:${VERSION}"
export IMG_LATEST="${REGISTRY}/whatap-operator:latest"

# Decide whether to also tag/push 'latest'.
# Dev/pre-release builds (e.g. 3.0.7-dev, 1.7.15-rc1) must NOT overwrite 'latest'.
TAG_LATEST=true
case $VERSION in
  *-dev*|*-rc*|*-alpha*|*-beta*|*-snapshot*|*-SNAPSHOT*)
    TAG_LATEST=false
    ;;
esac

echo "🚀 Building for $ARCH_MSG"
if [ "$TAG_LATEST" = true ]; then
  echo "🚀 Building and pushing both tags: ${IMG} and ${IMG_LATEST}"
else
  echo "🚀 Dev/pre-release build detected — pushing only: ${IMG} (skipping 'latest')"
fi

# Create a temporary Dockerfile.cross for multi-platform build
cat > Dockerfile.cross << 'EOF'
# Build the manager binary
FROM --platform=${BUILDPLATFORM} golang:1.24.3 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/ internal/

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
ARG VERSION=dev
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" \
    -o manager cmd/main.go
# Use alpine to support curl
FROM alpine:latest
WORKDIR /
RUN apk add --no-cache curl ca-certificates
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
EOF

# Create or use existing buildx builder
if ! docker buildx inspect whatap-operator-builder &>/dev/null; then
  docker buildx create --name whatap-operator-builder
fi
docker buildx use whatap-operator-builder

# Assemble tag arguments (always version; 'latest' only for release builds)
TAG_ARGS=(--tag "${IMG}")
if [ "$TAG_LATEST" = true ]; then
  TAG_ARGS+=(--tag "${IMG_LATEST}")
fi

# Build with the selected tags in a single command
docker buildx build --push \
  --platform=${PLATFORMS} \
  --build-arg VERSION=${VERSION} \
  --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  "${TAG_ARGS[@]}" \
  -f Dockerfile.cross .

# Clean up
rm Dockerfile.cross

echo "✅ 빌드 및 푸시 완료: $ARCH_MSG"
