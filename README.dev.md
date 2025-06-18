# 개발자 가이드

이 문서는 whatap-operator 프로젝트의 개발 및 테스트 프로세스를 최적화하기 위한 도구와 스크립트를 설명합니다.

## 빠른 CRD 검증

CRD(Custom Resource Definition) 변경 사항을 검증하기 위해 전체 빌드 프로세스를 실행할 필요 없이 빠르게 검증할 수 있는 스크립트를 제공합니다.

### validate-crd.sh

이 스크립트는 CRD 변경 사항을 빠르게 검증합니다:

```bash
./validate-crd.sh
```

**기능**:
- Go 타입 정의에서 CRD 매니페스트 생성
- kubectl을 사용하여 CRD 구문 및 스키마 검증 (dry-run 모드)
- 오류 발생 시 상세한 피드백 제공

**요구 사항**:
- kubectl이 설치되어 있어야 함

## 개발용 빌드

개발 중에 빠른 빌드 및 테스트를 위한 스크립트를 제공합니다.

### dev-build.sh

이 스크립트는 개발 목적으로 단일 아키텍처(amd64)에 대해서만 빌드하여 빌드 시간을 크게 단축합니다:

```bash
./dev-build.sh <VERSION> [--no-push]
```

**예시**:
```bash
./dev-build.sh 1.7.15-dev
./dev-build.sh 1.7.15-dev --no-push
```

**기능**:
- 단일 아키텍처(amd64)에 대해서만 빌드
- 로컬 Docker 데몬에 이미지 로드
- 선택적으로 레지스트리에 이미지 푸시
- 컬러 출력으로 가독성 향상

**요구 사항**:
- Docker가 설치되어 있어야 함
- Docker BuildKit이 활성화되어 있어야 함

## 프로덕션 빌드

프로덕션 릴리스를 위한 전체 멀티 아키텍처 빌드가 필요한 경우 기존 빌드 스크립트를 사용합니다:

```bash
./build.sh <VERSION> [<ARCH>]
```

**예시**:
```bash
./build.sh 1.7.15
./build.sh 1.7.15 amd64
```

## 개발 워크플로우

1. Go 타입 정의 변경
2. `./validate-crd.sh`로 CRD 검증
3. 필요한 경우 `./dev-build.sh <VERSION> --no-push`로 로컬 테스트용 이미지 빌드
4. 테스트 및 디버깅
5. 최종 릴리스를 위해 `./build.sh <VERSION>`으로 모든 아키텍처에 대한 이미지 빌드

이 워크플로우는 개발 주기를 크게 단축하고 피드백 루프를 빠르게 만들어 줍니다.