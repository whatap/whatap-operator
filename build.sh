#!/usr/bin/env bash
set -euo pipefail

# Display usage information
function show_usage {
  echo "❗ 사용법: ./build.sh <VERSION> [<ARCH>]"
  echo "  <VERSION>: 빌드할 버전 (예: 1.7.15)"
  echo "  <ARCH>: 빌드할 아키텍처 (옵션: amd64, arm64, all) [기본값: all]"
  echo "예: ./build.sh 1.7.15 arm64"
}

# Check if at least one argument is provided
if [ $# -lt 1 ]; then
  show_usage
  exit 1
fi

VERSION=$1
ARCH=${2:-all}  # Default to 'all' if not specified

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
    PLATFORMS="linux/arm64,linux/amd64,linux/s390x,linux/ppc64le"
    ARCH_MSG="all architectures (linux/arm64, linux/amd64, linux/s390x, linux/ppc64le)"
    ;;
  *)
    echo "❗ 지원하지 않는 아키텍처입니다: $ARCH"
    show_usage
    exit 1
    ;;
esac

export IMG="public.ecr.aws/whatap/whatap-operator:${VERSION}"
export IMG_LATEST="public.ecr.aws/whatap/whatap-operator:latest"

echo "🚀 Building for $ARCH_MSG"
echo "🚀 Building and pushing both tags: ${IMG} and ${IMG_LATEST}"

# Create a temporary Dockerfile.cross for multi-platform build
sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross

# Create or use existing buildx builder
if ! docker buildx inspect whatap-operator-builder &>/dev/null; then
  docker buildx create --name whatap-operator-builder
fi
docker buildx use whatap-operator-builder

# Build and push with both tags in a single command
docker buildx build --push \
  --platform=${PLATFORMS} \
  --build-arg VERSION=${VERSION} \
  --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  --tag ${IMG} \
  --tag ${IMG_LATEST} \
  -f Dockerfile.cross .

# Clean up
rm Dockerfile.cross

echo "✅ Build and push completed for $ARCH_MSG"
