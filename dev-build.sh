#!/usr/bin/env bash
set -euo pipefail

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

# Display usage information
function show_usage {
  echo -e "${YELLOW}❗ 사용법: ./dev-build.sh <VERSION> [--no-push]${NC}"
  echo -e "  <VERSION>: 빌드할 버전 (예: 1.7.15-dev)"
  echo -e "  --no-push: 이미지를 레지스트리에 푸시하지 않음 (선택 사항)"
  echo -e "예: ./dev-build.sh 1.7.15-dev"
}

# Check if at least one argument is provided
if [ $# -lt 1 ]; then
  show_usage
  exit 1
fi

VERSION=$1
PUSH="true"

# Check for --no-push flag
if [[ "$*" == *"--no-push"* ]]; then
  PUSH="false"
fi

# Set the platform to amd64 only for faster builds
PLATFORMS="linux/amd64"
ARCH_MSG="amd64 (개발용 빌드)"

# Set the image name with a dev suffix
export IMG="public.ecr.aws/whatap/whatap-operator:${VERSION}"

echo -e "${YELLOW}🚀 Building for $ARCH_MSG${NC}"
echo -e "${YELLOW}🚀 export IMG=${IMG}${NC}"

# Create a temporary Dockerfile for the build
echo -e "${YELLOW}📝 Creating temporary Dockerfile...${NC}"
sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.dev

# Build the image
echo -e "${YELLOW}🔨 Building image...${NC}"
docker buildx build \
  --platform=${PLATFORMS} \
  --build-arg VERSION=${VERSION} \
  --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  --tag ${IMG} \
  --load \
  -f Dockerfile.dev .

# Push the image if requested
if [ "$PUSH" = "true" ]; then
  echo -e "${YELLOW}📤 Pushing image to registry...${NC}"
  docker push ${IMG}
else
  echo -e "${YELLOW}ℹ️ Skipping push to registry (--no-push specified)${NC}"
fi

# Clean up
rm Dockerfile.dev

echo -e "${GREEN}✅ Development build completed for $ARCH_MSG${NC}"
echo -e "${GREEN}👉 Image: ${IMG}${NC}"