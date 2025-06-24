#!/usr/bin/env bash
set -euo pipefail

# Display usage information
function show_usage {
  echo "❗ 사용법: ./build.sh <VERSION> [<REGISTRY>] [--manifest-only]"
  echo "  <VERSION>: 빌드할 버전 (예: 1.7.15)"
  echo "  <REGISTRY>: 사용할 레지스트리 (기본값: public.ecr.aws/whatap)"
  echo "  --manifest-only: 이미지를 빌드하지 않고 매니페스트만 생성 (레지스트리에 아키텍처별 이미지가 이미 존재해야 함)"
  echo ""
  echo "예시:"
  echo "  ./build.sh 1.7.15"
  echo "  ./build.sh 1.7.15 docker.io/myuser"
  echo "  ./build.sh 1.7.15 --manifest-only"
  echo "  ./build.sh 1.7.15 docker.io/myuser --manifest-only"
}

# --- Argument Parsing ---
if [ $# -lt 1 ]; then
  show_usage
  exit 1
fi

VERSION=$1
REGISTRY="public.ecr.aws/whatap" # Default registry
MANIFEST_ONLY=false

# Handle optional arguments more robustly
for arg in "$@"; do
  shift
  case "$arg" in
    "--manifest-only")
      MANIFEST_ONLY=true
      ;;
    *)
      # Assume it's the registry if it's not the version or the flag
      if [[ "$arg" != "$VERSION" ]]; then
        REGISTRY=$arg
      fi
      ;;
  esac
done


# --- Configuration ---
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
PLATFORMS="linux/arm64,linux/amd64"
ARCH_MSG="linux/arm64, linux/amd64"

# Set image names with the specified registry
export IMG="${REGISTRY}/whatap-operator:${VERSION}"
export IMG_LATEST="${REGISTRY}/whatap-operator:latest"
export IMG_AMD64="${REGISTRY}/whatap-operator:${VERSION}-amd64"
export IMG_ARM64="${REGISTRY}/whatap-operator:${VERSION}-arm64"


# --- Helper Functions ---
function create_and_push_manifest() {
  local tag_to_create="$1"
  echo "🔨 Creating and pushing manifest for ${tag_to_create}..."

  # Create manifest using architecture-specific images
  # This is a safer and clearer way than using 'eval' and '--amend' repeatedly.
  docker manifest create "${tag_to_create}" "${IMG_AMD64}" "${IMG_ARM64}"

  # Annotate to ensure the correct architecture is chosen by clients
  docker manifest annotate "${tag_to_create}" "${IMG_AMD64}" --os linux --arch amd64
  docker manifest annotate "${tag_to_create}" "${IMG_ARM64}" --os linux --arch arm64

  docker manifest push "${tag_to_create}"
}


# --- Main Logic ---
if [ "$MANIFEST_ONLY" = true ]; then
  echo "🚀 매니페스트 전용 모드: 기존 이미지를 사용하여 매니페스트만 생성합니다."
  echo "  - amd64 이미지: ${IMG_AMD64}"
  echo "  - arm64 이미지: ${IMG_ARM64}"

  create_and_push_manifest "${IMG}"
  create_and_push_manifest "${IMG_LATEST}"

else
  echo "🚀 Building for architectures: $ARCH_MSG"
  echo "🚀 Building and pushing tags: ${IMG} and ${IMG_LATEST}"

  # --- Pre-compile Go Binaries ---
  mkdir -p bin
  echo "📦 Pre-compiling Go binaries in parallel..."

  # Compile for amd64
  echo "🔨 Compiling for linux/amd64..."
  (
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
      -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" \
      -o bin/manager.linux.amd64 cmd/main.go
  ) &
  AMDPID=$!

  # Compile for arm64
  echo "🔨 Compiling for linux/arm64..."
  (
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
      -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" \
      -o bin/manager.linux.arm64 cmd/main.go
  ) &
  ARMPID=$!

  # Wait for compilation to finish and check for errors
  wait $AMDPID && echo "✅ amd64 build completed" || (echo "❌ amd64 build failed" && exit 1)
  wait $ARMPID && echo "✅ arm64 build completed" || (echo "❌ arm64 build failed" && exit 1)


  # --- Create temporary Dockerfile ---
  # This improved Dockerfile uses ARG to copy the correct binary dynamically
  cat > Dockerfile.multi << EOF
# Use distroless as minimal base image
FROM gcr.io/distroless/static:nonroot

# Use build arguments to select the correct binary for the target platform
ARG TARGETOS
ARG TARGETARCH

# Copy the pre-compiled binary that matches the target architecture
COPY bin/manager.\${TARGETOS}.\${TARGETARCH} /manager

USER 65532:65532
ENTRYPOINT ["/manager"]
EOF

  # --- Build and Push Images ---
  # Create or use existing buildx builder
  if ! docker buildx inspect whatap-operator-builder &>/dev/null; then
    docker buildx create --name whatap-operator-builder
  fi
  docker buildx use whatap-operator-builder

  # Build, push, and create manifest in a single command
  echo "🔨 Building and pushing multi-arch images and manifests..."
  docker buildx build --push \
    --platform "${PLATFORMS}" \
    --tag "${IMG}" \
    --tag "${IMG_LATEST}" \
    -f Dockerfile.multi .

  # Also tag and push architecture-specific images for the manifest-only mode
  echo "🔨 Tagging and pushing architecture-specific images..."
  docker buildx build --push --platform="linux/amd64" --tag "${IMG_AMD64}" -f Dockerfile.multi .
  docker buildx build --push --platform="linux/arm64" --tag "${IMG_ARM64}" -f Dockerfile.multi .

fi


# --- Cleanup ---
if [ "$MANIFEST_ONLY" = false ]; then
  echo "🧹 Cleaning up temporary files..."
  rm Dockerfile.multi
fi


# --- Summary ---
echo ""
echo "📋 Build Summary:"
echo "  Version: $VERSION"
echo "  Registry: $REGISTRY"
echo "  Architectures: $ARCH_MSG"
echo "  Multi-arch Images:"
echo "    - $IMG"
echo "    - $IMG_LATEST"
echo "  Architecture-specific Images:"
echo "    - ${IMG_AMD64}"
echo "    - ${IMG_ARM64}"
echo ""

if [ "$MANIFEST_ONLY" = true ]; then
  echo "✅ 매니페스트 생성 및 푸시 완료!"
else
  echo "✅ 빌드 및 푸시 완료!"
fi
echo "🎉 작업이 성공적으로 완료되었습니다."

