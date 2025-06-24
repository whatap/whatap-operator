#!/usr/bin/env bash
set -euo pipefail

# Display usage information
function show_usage {
  echo "❗ 사용법: ./build.sh <VERSION>"
  echo "  <VERSION>: 빌드할 버전 (예: 1.7.15)"
  echo "예: ./build.sh 1.7.15"
}

# Check if at least one argument is provided
if [ $# -lt 1 ]; then
  show_usage
  exit 1
fi

AGENT_VERSION=$1
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)

# Login to AWS ECR Public
echo "🔐 Logging in to AWS ECR Public..."
aws ecr-public get-login-password --region us-east-1 | docker login --username AWS --password-stdin public.ecr.aws/whatap

# Pre-compile binaries for fast multi-platform build
echo "📦 Pre-compiling binaries for different architectures..."
make build-fast

# Build and push whatap-operator images for both architectures (fast approach)
echo "🔨 Building and pushing whatap-operator images using fast approach..."

# Create or use existing buildx builder
if ! docker buildx inspect whatap-operator-builder &>/dev/null; then
  echo "📦 Creating buildx builder..."
  docker buildx create --name whatap-operator-builder
fi
docker buildx use whatap-operator-builder

# Build and push amd64 image using fast Dockerfile
echo "🔨 Building and pushing amd64 image..."
docker buildx build --push \
  --platform=linux/amd64 \
  --build-arg VERSION=${AGENT_VERSION} \
  --build-arg BUILD_TIME=${BUILD_TIME} \
  --tag public.ecr.aws/whatap/whatap-operator:${AGENT_VERSION}-amd64 \
  -f Dockerfile.fast .

# Build and push arm64 image using fast Dockerfile
echo "🔨 Building and pushing arm64 image..."
docker buildx build --push \
  --platform=linux/arm64 \
  --build-arg VERSION=${AGENT_VERSION} \
  --build-arg BUILD_TIME=${BUILD_TIME} \
  --tag public.ecr.aws/whatap/whatap-operator:${AGENT_VERSION}-arm64 \
  -f Dockerfile.fast .

# Handle whatap-operator images for public ECR
echo "📥 Pulling whatap-operator images..."
docker pull -q public.ecr.aws/whatap/whatap-operator:${AGENT_VERSION}-amd64
docker pull -q public.ecr.aws/whatap/whatap-operator:${AGENT_VERSION}-arm64

# Check if manifest exists and handle it for whatap-operator
echo "🔍 Checking if manifest exists for whatap-operator:${AGENT_VERSION}..."
OPERATOR_MANIFEST=$(docker manifest inspect public.ecr.aws/whatap/whatap-operator:${AGENT_VERSION} 2>&1 || true)

# Check if "no such manifest" string is included
if echo "$OPERATOR_MANIFEST" | grep -q "no such manifest"; then
  echo "whatap-operator 매니페스트가 존재하지 않습니다. 삭제를 건너뜁니다."
else
  echo "whatap-operator 매니페스트가 존재합니다. 삭제를 진행합니다."
  docker manifest rm public.ecr.aws/whatap/whatap-operator:${AGENT_VERSION}
fi

# Create manifest for whatap-operator versioned tag
echo "📦 Creating manifest for whatap-operator:${AGENT_VERSION}..."
docker manifest create \
public.ecr.aws/whatap/whatap-operator:${AGENT_VERSION} \
--amend public.ecr.aws/whatap/whatap-operator:${AGENT_VERSION}-amd64 \
--amend public.ecr.aws/whatap/whatap-operator:${AGENT_VERSION}-arm64

# Handle latest tag manifest for whatap-operator
echo "🔍 Checking if manifest exists for whatap-operator:latest..."
OPERATOR_LATEST_MANIFEST=$(docker manifest inspect public.ecr.aws/whatap/whatap-operator:latest 2>&1 || true)
if ! echo "$OPERATOR_LATEST_MANIFEST" | grep -q "no such manifest"; then
  echo "whatap-operator latest 매니페스트가 존재합니다. 삭제를 진행합니다."
  docker manifest rm public.ecr.aws/whatap/whatap-operator:latest
fi

# Create manifest for whatap-operator latest tag
echo "📦 Creating manifest for whatap-operator:latest..."
docker manifest create \
public.ecr.aws/whatap/whatap-operator:latest \
--amend public.ecr.aws/whatap/whatap-operator:${AGENT_VERSION}-amd64 \
--amend public.ecr.aws/whatap/whatap-operator:${AGENT_VERSION}-arm64

# Push whatap-operator manifests
echo "🚀 Pushing whatap-operator manifests..."
docker manifest push public.ecr.aws/whatap/whatap-operator:${AGENT_VERSION}
docker manifest push public.ecr.aws/whatap/whatap-operator:latest


echo ""
echo "📋 Build Summary:"
echo "  Version: $AGENT_VERSION"
echo "  Registry: public.ecr.aws/whatap"
echo "  Architectures: linux/amd64, linux/arm64"
echo "  Images:"
echo "    - public.ecr.aws/whatap/whatap-operator:${AGENT_VERSION}"
echo "    - public.ecr.aws/whatap/whatap-operator:latest"
echo ""

echo "✅ 매니페스트 생성 및 푸시 완료: 멀티 아키텍처 (linux/amd64, linux/arm64)"
echo "🎉 The whatap-operator multi-architecture manifest creation was successful!"
