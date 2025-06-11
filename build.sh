#!/usr/bin/env bash
set -euo pipefail

# Display usage information
function show_usage {
  echo "❗ 사용법: ./build.sh <VERSION> [<ARCH>] [--no-cache]"
  echo "  <VERSION>: 빌드할 버전 (예: 1.7.15)"
  echo "  <ARCH>: 빌드할 아키텍처 (옵션: amd64, arm64, all) [기본값: amd64]"
  echo "  --no-cache: 캐시를 사용하지 않고 빌드 (선택 사항)"
  echo "예: ./build.sh 1.7.15 arm64"
  echo "    ./build.sh 1.7.15 all --no-cache"
}[

# Check if at least one argument is provided
if [ $# -lt 1 ]; then
  show_usage
  exit 1
fi

VERSION=$1
ARCH=${2:-amd64}  # Default to 'amd64' for faster development builds
NO_CACHE=false

# Check for --no-cache flag
if [[ "$*" == *"--no-cache"* ]]; then
  NO_CACHE=true
fi

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
echo "🚀 export IMG=${IMG}"

# Set cache options based on NO_CACHE flag
CACHE_OPTS=""
if [ "$NO_CACHE" = true ]; then
  echo "🚀 Building without cache"
  CACHE_OPTS="--no-cache"
fi

# Build and tag in a single operation to avoid duplicate builds
echo "🚀 Building and tagging images..."
make docker-buildx VERSION="${VERSION}" PLATFORMS="${PLATFORMS}" \
  EXTRA_ARGS="${CACHE_OPTS} --tag ${IMG} --tag ${IMG_LATEST}"

echo "✅ Build and push completed for $ARCH_MSG"
