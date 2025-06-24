#!/usr/bin/env bash
set -euo pipefail

# Display usage information
function show_usage {
  echo "❗ 사용법: ./build.sh <VERSION> [<ARCH>] [<OPTIONS>]"
  echo "  <VERSION>: 빌드할 버전 (예: 1.7.15)"
  echo "  <ARCH>: 빌드할 아키텍처 (옵션: amd64, arm64, all) [기본값: all]"
  echo "  <OPTIONS>:"
  echo "    --local: 로컬에만 빌드하고 레지스트리에 푸시하지 않음"
  echo "    --registry=<REGISTRY>: 사용할 레지스트리 (기본값: public.ecr.aws/whatap)"
  echo "예: ./build.sh 1.7.15 arm64"
  echo "    ./build.sh 1.7.15 arm64 --local"
  echo "    ./build.sh 1.7.15 all --registry=docker.io/myuser"
}

# Check if at least one argument is provided
if [ $# -lt 1 ]; then
  show_usage
  exit 1
fi

VERSION=$1
ARCH=${2:-all}  # Default to 'all' if not specified

# Default values
PUSH_FLAG="--push"
REGISTRY="public.ecr.aws/whatap"

# Parse additional options
shift
if [ $# -ge 1 ]; then
  shift  # Skip ARCH if provided
  for arg in "$@"; do
    case $arg in
      --local)
        PUSH_FLAG="--load"
        ;;
      --registry=*)
        REGISTRY="${arg#*=}"
        ;;
      *)
        echo "❗ 알 수 없는 옵션: $arg"
        show_usage
        exit 1
        ;;
    esac
  done
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

# Set image names with the specified registry
export IMG="${REGISTRY}/whatap-operator:${VERSION}"
export IMG_LATEST="${REGISTRY}/whatap-operator:latest"

# Check if --load is used with multiple platforms (not supported by Docker)
if [ "$PUSH_FLAG" = "--load" ] && [[ "$PLATFORMS" == *","* ]]; then
  echo "❗ 오류: --local 옵션은 다중 아키텍처 빌드에서 사용할 수 없습니다."
  echo "   단일 아키텍처를 지정하세요 (예: amd64 또는 arm64)"
  exit 1
fi

echo "🚀 Building for $ARCH_MSG"
if [ "$PUSH_FLAG" = "--push" ]; then
  echo "🚀 Building and pushing both tags: ${IMG} and ${IMG_LATEST}"
else
  echo "🚀 Building locally (no push) with tags: ${IMG} and ${IMG_LATEST}"
fi

# Create a temporary Dockerfile.cross for multi-platform build
sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross

# Create or use existing buildx builder
if ! docker buildx inspect whatap-operator-builder &>/dev/null; then
  docker buildx create --name whatap-operator-builder
fi
docker buildx use whatap-operator-builder

# Build with both tags in a single command
set +e  # Don't exit on error for better error handling
docker buildx build ${PUSH_FLAG} \
  --platform=${PLATFORMS} \
  --build-arg VERSION=${VERSION} \
  --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  --tag ${IMG} \
  --tag ${IMG_LATEST} \
  -f Dockerfile.cross .
BUILD_RESULT=$?
set -e  # Restore exit on error

# Handle build errors
if [ $BUILD_RESULT -ne 0 ]; then
  echo "❗ 빌드 실패 (코드: $BUILD_RESULT)"

  if [ "$PUSH_FLAG" = "--push" ]; then
    echo "💡 팁: 레지스트리 인증 문제가 있을 수 있습니다. 다음 옵션을 시도해보세요:"
    echo "   1. 로컬 빌드만 하려면: ./build.sh $VERSION $ARCH --local"
    echo "   2. 다른 레지스트리 사용: ./build.sh $VERSION $ARCH --registry=docker.io/yourname"
    echo "   3. AWS ECR에 로그인: aws ecr-public get-login-password --region us-east-1 | docker login --username AWS --password-stdin public.ecr.aws"
  else
    echo "💡 팁: Docker 설정이나 디스크 공간을 확인하세요."
  fi

  exit $BUILD_RESULT
fi

# Clean up
rm Dockerfile.cross

if [ "$PUSH_FLAG" = "--push" ]; then
  echo "✅ 빌드 및 푸시 완료: $ARCH_MSG"
else
  echo "✅ 로컬 빌드 완료: $ARCH_MSG"
fi
