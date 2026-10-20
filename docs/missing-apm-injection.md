# APM 주입 누락 확인

Operator는 현재 APM 자동 주입 대상 조건에 맞지만 에이전트 init 컨테이너와 주입 결과 annotation이 모두 없는 파드를 찾아 `APMInjectionMissing` Warning 이벤트와 `APM injection missing` 로그를 남깁니다. 웹훅 요청이 Operator에 도달하지 않았어도 생성된 파드를 통해 확인할 수 있습니다.

```sh
kubectl get events -A --field-selector reason=APMInjectionMissing
kubectl describe pod <pod-name> -n <namespace>
```

이벤트에는 현재 일치하는 APM target 이름과 확인할 항목이 표시됩니다. API server의 웹훅 오류, API server에서 Operator까지의 TLS 인증서·프록시·네트워크 경로, 파드 생성 이후 대상 선택 조건이 변경되었는지 확인하세요. 이 경고 자체가 웹훅 호출 실패나 x509 오류의 원인을 확정하지는 않습니다.

파드 생성/갱신 시 확인하고, 결과 기록이 없는 파드는 5분 간격으로 현재 설정을 다시 확인합니다. Operator 재시작 시에도 기존 파드를 확인합니다. 동일 파드 UID의 이벤트가 남아 있는 동안에는 중복 생성하거나 같은 로그를 반복 출력하지 않습니다. Kubernetes가 이벤트를 만료시킨 뒤에도 누락 상태이면 다시 기록할 수 있습니다.

다음 파드는 제외합니다.

- `whatap-apm-injected: "true"` 또는 `whatap-apm-injection-status`/`whatap-apm-injection-reason` 값이 있는 파드
- 와탭 에이전트 init 컨테이너가 있는 파드
- 주입 기능 또는 대상이 비활성화되었거나, 파드/네임스페이스 선택 조건에 맞지 않는 파드
- WhatapAgent CR보다 먼저 생성된 파드, 삭제 중인 파드, 완료/실패한 파드, static Pod의 mirror 파드

현재 설정으로 판단하므로 생성 당시에는 대상이 아니었지만 이후 선택 조건이 바뀐 파드도 경고 대상이 될 수 있습니다. init 컨테이너 존재 여부만 확인하며, APM 프로세스의 정상 기동이나 수집 서버 연결까지 검증하지 않습니다. 조회 권한이나 API 오류가 발생하면 누락으로 단정하지 않고 재시도합니다.

감지는 파드를 수정하거나 재시작하지 않습니다. 원인을 해결한 뒤 필요한 워크로드를 재배포해 APM이 주입되는지 확인하세요. `failurePolicy: Ignore`와 기존 주입 동작은 유지됩니다. 웹훅 기능을 끈 Operator에서는 이 감지도 실행되지 않습니다.

개발/배포 참고: Pod metadata watch와 대상 Pod spec 직접 조회를 사용하며 전체 Pod spec 캐시를 추가하지 않습니다. 기존 Pod 조회·Event 생성 권한 외에 Namespace `get`이 필요합니다. 기본 배포 manifest의 core 리소스 권한에는 이미 포함되어 있으며, 생성되는 최소 RBAC에도 명시합니다. CRD 및 버전 변경은 없습니다.
