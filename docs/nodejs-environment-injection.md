# Node.js 자동 계측 환경변수 병합

## 지원 범위

Operator는 애플리케이션 컨테이너의 `env`에 명시된 `NODE_OPTIONS`와 `NODE_PATH`를 보존하면서 다음을 추가한다.

- `NODE_OPTIONS`: 와탭 preload 옵션 `-r whatap`
- `NODE_PATH`: 와탭 모듈 경로 `/whatap-agent/node_modules`

문자열 옵션의 공백·따옴표와 기존 모듈 경로를 유지한다. 같은 Pod 설정을 다시 처리해도 Operator가 추가한 설정이 계속 늘어나지 않는다. 같은 이름이 여러 번 선언되면 Kubernetes의 선언 순서와 마지막 대입을 존중한다.

`configMapKeyRef`·`secretKeyRef`의 필수 참조(`optional` 생략 또는 `false`)도 병합할 수 있다. 원래 참조를 내부 환경변수로 유지하고 kubelet이 컨테이너 시작 시 해석하게 한다. Operator가 ConfigMap·Secret 값을 추가로 조회하거나 Pod 사양·로그에 값을 기록하는 방식이 아니다.

## 선택적 참조의 안전 동작

선택적 참조(`optional: true`)는 같은 변수에 대한 앞선 문자열 선언 또는 필수 참조로 대체값의 존재가 보장될 때만 보강한다.

대체값이 보장되지 않으면 참조가 없을 때 `envFrom`이나 컨테이너 이미지의 `ENV`가 적용될 수 있다. Admission 시점에는 그 값을 안전하게 알 수 없으므로 **`NODE_OPTIONS`와 `NODE_PATH`의 자동 보강을 모두 생략하고 기존 설정을 유지한다.** 이 경우 자동 preload가 추가되지 않으며 다음 진단 로그를 확인할 수 있다.

```text
Skipping Node.js environment augmentation
reason: optional source fallback is not guaranteed; use explicit non-optional environment variables
```

자동 계측이 필요한 경우 실제로 존재하는 키를 필수 참조로 지정하거나, 필요한 기존 옵션·경로를 포함한 명시적 `env` 문자열을 사용한다. 없는 키를 무조건 필수 참조로 바꾸면 Kubernetes가 컨테이너 시작을 거부하므로 키 존재 여부를 먼저 확인한다.

`envFrom` 또는 이미지 `ENV`에만 있는 Node.js 변수는 이 병합 기능의 지원 대상이 아니다. 필요한 기존 설정을 컨테이너의 명시적 `env`에 포함해야 한다.

## 적용 및 검증

수정된 Operator 이미지로 업그레이드한 뒤, 승인된 애플리케이션 배포 절차에 따라 대상 Pod를 재생성한다. 이미 실행 중인 Pod의 환경변수가 소급 변경되는 것은 아니다.

Pod의 최종 환경변수, 기존 애플리케이션 preload·모듈 탐색 경로, 와탭의 실제 데이터 수집을 구분하여 확인한다. 저장소의 Node.js CLI 테스트는 작은 preload fixture로 런타임 동작을 검증하며, 실제 Node.js 에이전트·수집 서버 E2E를 대체하지 않는다.
