#!/usr/bin/env bash
set -euo pipefail

if [ $# -lt 1 ]; then
  echo "❗ VERSION 인자를 입력하세요."
  echo "예: ./build.sh 1.7.15"
  exit 1
fi

VERSION=$1
export IMG="public.ecr.aws/whatap/whatap-operator:${VERSION}"

echo "🚀 export IMG=${IMG}"
echo "🚀 make docker-build VERSION=${VERSION}"

make docker-build VERSION="${VERSION}"
docker tag public.ecr.aws/whatap/whatap-operator:${VERSION} public.ecr.aws/whatap/whatap-operator:latest
source ~/.aws-ecr-login
docker push public.ecr.aws/whatap/whatap-operator:latest