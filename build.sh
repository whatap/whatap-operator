#!/usr/bin/env bash
set -euo pipefail

# Display usage information
function show_usage {
  echo "❗ 사용법: ./build.sh <VERSION> [<REGISTRY>] [--manifest-only]"
  echo "  <VERSION>: 빌드할 버전 (예: 1.7.15)"
  echo "  <REGISTRY>: 사용할 레지스트리 (기본값: public.ecr.aws/whatap)"
  echo "  --manifest-only: 이미지를 빌드하지 않고 매니페스트만 생성 (기존 이미지 필요)"
  echo "예: ./build.sh 1.7.15"
  echo "    ./build.sh 1.7.15 docker.io/myuser"
  echo "    ./build.sh 1.7.15 docker.io/myuser --manifest-only"
}

# Check if at least one argument is provided
if [ $# -lt 1 ]; then
  show_usage
  exit 1
fi

VERSION=$1
REGISTRY=${2:-public.ecr.aws/whatap}  # Default registry
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)

# Check if --manifest-only flag is provided
MANIFEST_ONLY=false
for arg in "$@"; do
  if [ "$arg" = "--manifest-only" ]; then
    MANIFEST_ONLY=true
    break
  fi
done

# Always build for both architectures
PLATFORMS="linux/arm64,linux/amd64"
ARCH_MSG="linux/arm64, linux/amd64"

# Set image names with the specified registry
export IMG="${REGISTRY}/whatap-operator:${VERSION}"
export IMG_LATEST="${REGISTRY}/whatap-operator:latest"

if [ "$MANIFEST_ONLY" = true ]; then
  echo "🚀 매니페스트 전용 모드: 기존 이미지를 사용하여 매니페스트만 생성합니다"
  echo "🚀 대상 이미지: ${IMG} 및 ${IMG_LATEST}"
else
  echo "🚀 Building for both architectures: $ARCH_MSG"
  echo "🚀 Building and pushing both tags: ${IMG} and ${IMG_LATEST}"

  # Create bin directory if it doesn't exist
  mkdir -p bin

  # Pre-compile binaries for different architectures
  echo "📦 Pre-compiling binaries for different architectures..."

  # Compile binaries in parallel for better performance
  if [[ "$PLATFORMS" == *"linux/amd64"* ]]; then
    echo "🔨 Compiling for linux/amd64..."
    (
      CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
        -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" \
        -o bin/manager.linux.amd64 cmd/main.go
    ) &
    AMDPID=$!
    echo "  Started amd64 build process (PID: $AMDPID)"
  fi

  if [[ "$PLATFORMS" == *"linux/arm64"* ]]; then
    echo "🔨 Compiling for linux/arm64..."
    (
      CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
        -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" \
        -o bin/manager.linux.arm64 cmd/main.go
    ) &
    ARMPID=$!
    echo "  Started arm64 build process (PID: $ARMPID)"
  fi

  # Wait for all compilation processes to finish
  BUILD_SUCCESS=true

  if [[ "$PLATFORMS" == *"linux/amd64"* ]]; then
    echo "⏳ Waiting for amd64 build to complete..."
    if wait $AMDPID; then
      echo "✅ amd64 build completed"
    else
      echo "❌ amd64 build failed"
      BUILD_SUCCESS=false
    fi
  fi

  if [[ "$PLATFORMS" == *"linux/arm64"* ]]; then
    echo "⏳ Waiting for arm64 build to complete..."
    if wait $ARMPID; then
      echo "✅ arm64 build completed"
    else
      echo "❌ arm64 build failed"
      BUILD_SUCCESS=false
    fi
  fi

  # Exit if any builds failed
  if [ "$BUILD_SUCCESS" = false ]; then
    echo "❌ One or more builds failed. Exiting."
    exit 1
  fi

  # Create a temporary Dockerfile for multi-platform build
  cat > Dockerfile.multi << EOF
# Use distroless as minimal base image
FROM gcr.io/distroless/static:nonroot

# Copy the pre-compiled binary
COPY manager /manager

USER 65532:65532
ENTRYPOINT ["/manager"]
EOF

  # Create or use existing buildx builder
  if ! docker buildx inspect whatap-operator-builder &>/dev/null; then
    docker buildx create --name whatap-operator-builder
  fi
  docker buildx use whatap-operator-builder

  # Build and push images for each architecture separately
  echo "🔨 Building and pushing Docker images..."

  # Build and push for each architecture
  if [[ "$PLATFORMS" == *"linux/amd64"* ]]; then
    echo "🔨 Building and pushing amd64 image..."
    cp bin/manager.linux.amd64 manager
    docker buildx build --push \
      --platform=linux/amd64 \
      --tag ${IMG}-amd64 \
      -f Dockerfile.multi .
  fi

  if [[ "$PLATFORMS" == *"linux/arm64"* ]]; then
    echo "🔨 Building and pushing arm64 image..."
    cp bin/manager.linux.arm64 manager
    docker buildx build --push \
      --platform=linux/arm64 \
      --tag ${IMG}-arm64 \
      -f Dockerfile.multi .
  fi
fi

# Create and push manifest lists for both tags
echo "🔨 Creating and pushing manifest lists..."

# Create manifest list for version tag
MANIFEST_CMD="docker manifest create ${IMG}"
if [[ "$PLATFORMS" == *"linux/amd64"* ]]; then
  MANIFEST_CMD+=" --amend ${IMG}-amd64"
fi
if [[ "$PLATFORMS" == *"linux/arm64"* ]]; then
  MANIFEST_CMD+=" --amend ${IMG}-arm64"
fi

eval ${MANIFEST_CMD}
docker manifest push ${IMG}

# Create manifest list for latest tag
MANIFEST_CMD="docker manifest create ${IMG_LATEST}"
if [[ "$PLATFORMS" == *"linux/amd64"* ]]; then
  MANIFEST_CMD+=" --amend ${IMG}-amd64"
fi
if [[ "$PLATFORMS" == *"linux/arm64"* ]]; then
  MANIFEST_CMD+=" --amend ${IMG}-arm64"
fi

eval ${MANIFEST_CMD}
docker manifest push ${IMG_LATEST}

# Clean up
if [ "$MANIFEST_ONLY" = false ]; then
  rm Dockerfile.multi
  rm -f manager
fi

# Print summary
echo ""
echo "📋 Build Summary:"
echo "  Version: $VERSION"
echo "  Registry: $REGISTRY"
echo "  Architectures: $ARCH_MSG"
echo "  Images:"
echo "    - $IMG"
echo "    - $IMG_LATEST"
echo ""

if [ "$MANIFEST_ONLY" = true ]; then
  echo "✅ 매니페스트 생성 및 푸시 완료: 멀티 아키텍처 (linux/amd64, linux/arm64)"
  echo "🎉 The multi-architecture manifest creation was successful!"
else
  echo "✅ 빌드 및 푸시 완료: 멀티 아키텍처 (linux/amd64, linux/arm64)"
  echo "🎉 The pre-compiled multi-architecture image build was successful!"
fi
