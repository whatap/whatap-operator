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

echo "🚀 Building for $ARCH_MSG"
echo "🚀 export IMG=${IMG}"
echo "🚀 make docker-buildx VERSION=${VERSION} PLATFORMS=${PLATFORMS}"

# Build images for the specified architecture
make docker-buildx VERSION="${VERSION}" PLATFORMS="${PLATFORMS}"

# Tag the latest version
echo "🚀 Tagging latest version"
export IMG_LATEST="public.ecr.aws/whatap/whatap-operator:latest"
make docker-buildx VERSION="${VERSION}" IMG="${IMG_LATEST}" PLATFORMS="${PLATFORMS}"

echo "✅ Build and push completed for $ARCH_MSG"
