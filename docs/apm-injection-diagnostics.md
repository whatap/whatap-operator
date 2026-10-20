# APM 자동 주입 진단: 웹훅 호출 실패 감지

## 요약

Operator의 Pod 주입 웹훅(`mpod.kb.io`)은 실패 정책이 `Ignore`다. API server가 웹훅을 호출하지 못하면 Pod는 APM 에이전트 없이 그대로 생성된다. 요청이 Operator에 도달하지 않으므로 Operator 로그, Pod annotation, 이벤트에 아무것도 남지 않고, 오류는 API server 로그에만 남는다.

Operator는 이 상황을 두 가지로 기록한다.

| 대상 | 이벤트 | 의미 |
| --- | --- | --- |
| WhatapAgent `whatap` | `Warning ApmInjectionWebhookUnreachable` | API server가 웹훅을 호출하지 못한다. 메시지에 API server가 돌려준 오류가 들어 있다. |
| WhatapAgent `whatap` | `Normal ApmInjectionWebhookRecovered` | 다시 호출할 수 있다. |
| Pod | `Warning ApmInjectionWebhookNotCalled` | APM 대상인데 웹훅을 거치지 않고 생성되어 주입되지 않았다. |

모두 Kubernetes 이벤트이므로 와탭 Kubernetes 에이전트의 이벤트 수집 대상이다. WhatapAgent처럼 클러스터 범위 리소스의 이벤트는 `default` 네임스페이스에 생성된다.

## 웹훅 호출 점검

- APM 자동 주입이 켜져 있고(`features.apm.instrumentation.enabled: true`) 활성 대상이 하나 이상 있을 때만 동작한다.
- 리더 Operator가 시작 1분 뒤부터 5분마다 Operator 네임스페이스에 dry-run Pod 생성 요청을 보낸다. dry-run이므로 Pod는 저장·스케줄되지 않는다.
- 점검 Pod에는 `monitoring.whatap.com/apm-webhook-probe: "true"` 라벨이 붙는다. MutatingWebhookConfiguration `whatap-webhook`의 `apm-probe.monitoring.whatap.com` 항목은 이 라벨이 있는 Pod만 대상으로 하며 실패 정책이 `Fail`이다. 그래서 호출이 실패하면 API server가 오류를 점검 요청에 그대로 돌려준다. 애플리케이션 Pod는 이 항목의 대상이 아니다.
- 점검 Pod는 `mpod.kb.io`와 같은 서비스·경로·CA로 호출되므로 애플리케이션 Pod와 같은 경로를 점검한다.
- 연속 2회 실패하면 Operator 로그에 오류를 남기고 WhatapAgent에 Warning 이벤트를 기록한다. Operator 재시작 직후(웹훅 CA 재생성, 엔드포인트 교체)의 일시 실패는 보고하지 않기 위해서다. 실패가 계속되면 1시간마다, 원인이 바뀌면 즉시 다시 기록한다.
- 점검 Pod가 다른 이유(권한, ResourceQuota, 정책 엔진 등)로 거부되면 판단을 보류하고 로그만 남긴다.

## 주입 누락 Pod 표시

- 웹훅은 처리한 모든 Pod에 표시를 남긴다. 주입하면 `whatap-apm-injected: "true"`, 건너뛰거나 실패하면 `whatap-apm-injection-status`다.
- APM 대상(활성 target의 `namespaceSelector`·`podSelector`에 맞는 Pod)인데 두 표시가 모두 없으면 웹훅을 거치지 않고 생성된 Pod다. 이런 Pod에 Warning 이벤트를 남긴다.
- Operator 프로세스가 시작된 뒤 생성된 Pod만 본다. 3.0.19 이전 Operator는 건너뛴 Pod에 표시를 남기지 않았으므로 그 전에 만들어진 Pod는 구분할 수 없다.

## 확인 방법

```bash
# 호출 실패와 원인
kubectl describe whatapagent whatap
kubectl logs -n <operator-namespace> deploy/whatap-operator | grep -i "APM injection webhook"

# 주입되지 않은 Pod
kubectl get events -A --field-selector reason=ApmInjectionWebhookNotCalled
```

## 자주 있는 원인

| API server 오류 | 원인 | 조치 |
| --- | --- | --- |
| `x509: certificate signed by unknown authority` | API server의 HTTPS 프록시가 클러스터 내부 TLS 연결을 가로챈다 | API server의 `NO_PROXY`에 `.svc`를 추가한다 |
| `no endpoints available for service "whatap-admission-controller"` | Operator Pod가 없거나 준비되지 않았다 | Operator Pod 상태를 확인한다 |
| `context deadline exceeded`, `connection refused` | API server와 Operator Pod 사이 통신이 막혔다(NetworkPolicy, 방화벽 등) | API server에서 Operator Pod 9443 포트로의 통신을 허용한다 |

원인을 고친 뒤 `ApmInjectionWebhookNotCalled` 이벤트가 남은 Pod를 재시작해야 APM이 주입된다.

## 권한

Helm 차트의 Operator ClusterRole에는 이미 포함되어 있다. RBAC를 직접 관리한다면 Operator에 다음 권한을 부여한다.

- `pods`: `create` (dry-run 점검)
- `namespaces`: `get`, `list`, `watch`
- `events`: `create`

## 제한 사항

- 감지만 한다. 이미 생성된 Pod는 재시작해야 주입된다.
- Operator가 실행 중이지 않을 때(설치 전, 재시작·업그레이드 사이) 생성된 Pod는 Pod 이벤트 대상이 아니다.
- 점검 Pod가 정책 엔진 등에 거부되는 클러스터에서는 웹훅 호출 점검 결과를 판단할 수 없다. 이때도 주입 누락 Pod 표시는 동작한다.

## 관련 코드

- `internal/webhook/v2alpha1/apm_webhook_probe.go`: 웹훅 호출 점검
- `internal/webhook/v2alpha1/apm_injection_missed_event_controller.go`: 주입 누락 Pod 이벤트
- `internal/controller/apm_probe_webhook.go`: 점검용 웹훅 항목
