#!/usr/bin/env bash
set -euo pipefail

# Display usage information
function show_usage {
  echo "❗ 사용법: ./create-manifest.sh <VERSION> [<REGISTRY>]"
  echo "  <VERSION>: 매니페스트를 생성할 버전 (예: 1.9.78)"
  echo "  <REGISTRY>: 사용할 레지스트리 (기본값: public.ecr.aws/whatap)"
  echo "예: ./create-manifest.sh 1.9.78"
  echo "    ./create-manifest.sh 1.9.78 docker.io/myuser"
}

# Check if at least one argument is provided
if [ $# -lt 1 ]; then
  show_usage
  exit 1
fi

VERSION=$1
REGISTRY=${2:-public.ecr.aws/whatap}  # Default registry

# Set image names
IMG="${REGISTRY}/whatap-operator:${VERSION}"
IMG_LATEST="${REGISTRY}/whatap-operator:latest"

echo "🚀 Creating manifest list for version: ${VERSION}"
echo "🚀 Using registry: ${REGISTRY}"
echo "🚀 Source images:"
echo "   - ${IMG}-amd64"
echo "   - ${IMG}-arm64"
echo "🚀 Target images:"
echo "   - ${IMG}"

# Create and push manifest list for version tag
echo "🔨 Creating and pushing manifest list for ${IMG}..."
docker manifest create ${IMG} ${IMG}-amd64 ${IMG}-arm64
docker manifest push ${IMG}

echo "✅ 매니페스트 생성 및 푸시 완료: ${IMG}"
echo "🎉 The multi-architecture manifest creation was successful!"