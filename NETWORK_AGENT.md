# Network agent (개발 기능)

`spec.features.networkAgent`는 기존 Java node agent / Go node helper와 **별개인**
`whatap-network-agent` DaemonSet을 관리한다. 기본값은 비활성화이며 기존 설치를
자동으로 변경하지 않는다. 컨테이너를 기존 node agent Pod에 수동 추가하지 않는다.

> 이 필드는 이 소스 변경이 포함된 **operator 이미지와 CRD를 모두 배포한 뒤** 사용할
> 수 있다. 이전 operator에서는 동작하지 않고, 이전 CRD는 필드를 제거할 수 있다.
> 아래 YAML을 적용하는 것만으로 operator 자체가 업그레이드되지는 않는다.

## 기존 WhatapAgent YAML에 추가

기존 `spec.license`, `spec.host`, `spec.port`, `features.k8sAgent`, `features.apm`,
`features.openAgent`를 유지하고 `spec.features` 아래에 다음 블록을 추가한다.
먼저 한 노드에서 검증하고 나중에 `nodeSelector` 범위를 확대한다.

```yaml
spec:
  features:
    networkAgent:
      enabled: true
      image: public.ecr.aws/whatap/network_agent_dev@sha256:97d19e79ff3a1a89039c0d9f54ea8fdfa40745efc63d48f4706bfef084319421
      credentialsSecretName: whatap-credentials
      serviceAccountName: whatap
      nodeSelector:
        kubernetes.io/hostname: worker01.okd4.cluster.local
      resources:
        requests:
          cpu: 50m
          memory: 128Mi
        limits:
          cpu: 500m
          memory: 512Mi
```

이 예제의 이미지는 **개발 이미지**이며 digest로 고정했다. 릴리스 승인이나 모든 커널의
호환성 보증을 의미하지 않는다. `serviceAccountName: whatap`은 해당 계정의 Pod
list/watch와 필요한 SCC 권한을 **이미 승인한 클러스터에서만** 사용한다.

동봉된 `config/samples/network-agent-enable-patch.yaml`은 전체 CR 교체용이 아닌
JSON merge patch 형식이다. 실제 CR 이름을 먼저 확인한 후 적용한다.

```sh
kubectl get whatapagent
kubectl patch whatapagent whatap --type=merge \
  --patch-file=config/samples/network-agent-enable-patch.yaml
kubectl -n whatap-monitoring rollout status daemonset/whatap-network-agent
```

Helm/GitOps가 WhatapAgent를 관리한다면 live patch만 하지 말고 해당 원본 YAML에도
같은 블록을 반영한다. namespace는 operator의 기존 기본 namespace 설정을 따른다.

## 인증·권한·호스트 접근

- `credentialsSecretName`의 기본값은 `whatap-credentials`. 동일 namespace의 Secret에
  `WHATAP_LICENSE`, `WHATAP_HOST`, `WHATAP_PORT` 키가 필요하다.
- Secret 값은 Pod의 `secretKeyRef`로만 참조한다. 각각 실행 파일의
  `WHATAP_ACCESSKEY`, `WHATAP_SERVER_HOST`, `WHATAP_SERVER_PORT`에 대응한다.
  CR의 평문 license나 ConfigMap으로 복사하지 않는다.
- `NODE_NAME`은 Downward API의 `spec.nodeName`을 사용하고 object name은
  `network-$(NODE_NAME)`이다. 기존 Java agent의 object identity를 재사용하지 않는다.
- Pod UID 수집은 기본 활성화한다. ServiceAccount token과 클러스터 범위의 core
  Pod `get/list/watch` 권한이 필요하며, informer 초기 동기화 실패는 시작 실패다.
- `serviceAccountName`을 생략하면 전용 SA와 최소 Pod 조회 RBAC을 operator가 관리한다.
  직접 지정하면 해당 SA/RBAC은 외부 관리로 보고 만들거나 수정하지 않는다.
- OpenShift/OKD의 SCC 권한은 **자동으로 부여하지 않는다.** 전용 SA를 사용할 경우
  관리자가 필요한 SCC 승인을 별도로 해야 한다. 기존 운영 SCC/RBAC의 무단 확대를
  피하고, 사용 가능한 SCC와 실제 Pod의 `openshift.io/scc` annotation을 함께 검증한다.
- 현재 개발 collector는 root/privileged, hostPID/hostNetwork를 사용한다. 읽기 전용
  hostPath는 `/sys/kernel/tracing`, `/sys/kernel/debug`, `/sys/kernel/btf`뿐이다.
  host root나 runtime socket은 마운트하지 않는다. 커널 BTF/tracepoint 지원도 필요하다.
- `resources`, `nodeSelector`, `tolerations`, `imagePullSecrets`로 실행 범위를 설정한다.
  requests와 limits를 함께 설정하고 실측 부하로 조정한다. Linux 노드만 선택하며,
  명시적인 다른 OS 설정은 거부한다. Go 메모리 제한은 유효한 container memory limit의
  75%로 설정한다. 이 값은 Go 메모리의 soft limit이며 전체 RSS/BPF 메모리 보증이 아니다.
  0 이하의 limit이나 limit보다 큰 request는 리소스를 만들기 전에 거부한다.

## 로그량 설정

**이 로그 옵션을 지원하는 network agent 이미지와 operator/CRD가 모두 필요하다.**
위 예제의 기존 `97d19e…` 이미지에는 이 옵션이 없으므로, 같은 이미지에 옵션만
추가하면 시작에 실패한다. 지원 이미지를 준비한 뒤 다음 필드를 추가한다.

```yaml
spec:
  features:
    networkAgent:
      stdout: none
      logLevel: info
      logInterval: 1m
```

- `stdout`: `auto`, `jsonl`, `none`. 수집 데이터 JSONL의 stdout 복제만 제어하며
  TagCount 전송과 UID 수집은 유지한다. 지원 이미지의 기본 `auto`는 직접 전송 시
  `none`이다. 원시/집계 JSONL 증거가 필요한 진단에서만 `jsonl`을 사용한다.
- `logLevel`: `debug`, `info`, `warn`, `error`. 기본 `info`에서 전송 카운터 요약을
  stderr에 출력한다. `debug`만으로 원시 수집 데이터를 켜지는 않는다.
- `logInterval`: 기본 `1m`; `0s`는 주기 요약을 끈다. `warn`/`error`도 주기 요약을
  끄지만 치명 오류와 종료 시 최종 전송 요약은 유지한다.
- `written`은 TCP 쓰기 완료이며 backend 저장 ACK가 아니다. 요약에는 트래픽 본문,
  명령행, 인증정보를 포함하지 않는다.
- 필드를 생략하면 operator는 새 CLI 인자를 넣지 않아 기존 이미지와 호환된다.
  로그 옵션 변경은 network DaemonSet을 rollout하므로 실행 중인 수집기는 재시작된다.
  이 옵션은 수집 서버 연결 실패 시 종료/재연결 정책을 변경하지 않는다.

### 환경 변수와 외부 설정

`networkAgent.env`는 Kubernetes `EnvVar` 배열(literal / `valueFrom`), `envFrom`은
ConfigMap/Secret source 배열이다. 참조는 network agent Pod namespace에서 해석된다.
배열 순서와 중복 항목을 보존한다. `envFrom`은 뒤 source가 우선하며 명시적 `env`가
`envFrom`보다 우선한다. operator는 source 값을 조회하거나 로그에 출력하지 않는다.

- 새 collector 이미지에서 `WHATAP_STDOUT`, `WHATAP_LOG_LEVEL`,
  `WHATAP_LOG_INTERVAL`을 사용할 수 있다. 기존 개발 digest는 env 기반 로그 설정을
  지원하지 않는다. operator/CRD와 **env 지원 collector 이미지**가 모두 필요하다.
- 우선순위: 명시적 `stdout`/`logLevel`/`logInterval` 필드(CLI) > 환경 변수 > 이미지 기본값.
  환경 변수만 사용하려면 기존 CR의 해당 로그 필드를 제거한다. 생략 시 새 flag는 없다.
- source는 Pod 실행 시 해석되므로 유효 로그 값 검증은 collector가 실행 시 수행한다.
  잘못된 유효 값은 시작 실패이며 operator가 임의로 기본값으로 바꾸지 않는다.
- `NODE_NAME`, `WHATAP_OBJECT_NAME`, `WHATAP_ACCESSKEY`, `WHATAP_SERVER_HOST`,
  `WHATAP_SERVER_PORT`, `GOMAXPROCS`, `GOMEMLIMIT`는 operator 관리 이름이다.
  직접 `env`에 쓰면 SA/RBAC/DaemonSet 쓰기 전에 거부한다. `envFrom`의 같은 이름은
  operator의 명시적 env가 우선하므로 관리 값을 덮어쓸 수 없다.
- env/envFrom 생략 시 기존 Pod template을 유지한다. 변경은 Pod rollout을 유발한다.
  ConfigMap/Secret 값만 수정하면 기존 Pod 환경이 자동 갱신되지는 않는다.

`config/samples/network-agent-logging-env-patch.yaml`은 검토용 merge patch이다.
기본 예제는 외부 리소스 없이 `auto`/`info`/`1m`을 literal env로 지정하고 CLI 필드를
제거한다. `env` 배열은 통째로 교체되므로 기존 사용자 항목을 합친 후 사용해야 한다.
예제에 없는 기존 `envFrom`은 유지된다. 직접 `envFrom` 배열을 지정하면 그것도 교체된다.

외부 ConfigMap을 사용할 때는 다음처럼 지정할 수도 있다. 이 조각은 전체 CR이 아니며,
기존 CLI 로그 필드는 제거해야 한다. `network-agent-logging` ConfigMap을 Pod namespace에
준비하고 `WHATAP_STDOUT`, `WHATAP_LOG_LEVEL`, `WHATAP_LOG_INTERVAL` 문자열 키를 넣는다.

```yaml
spec:
  features:
    networkAgent:
      envFrom:
        - configMapRef:
            name: network-agent-logging
      # 개별 키만 가져오는 valueFrom 방식도 지원한다.
      env:
        - name: WHATAP_LOG_LEVEL
          valueFrom:
            configMapKeyRef:
              name: network-agent-logging
              key: WHATAP_LOG_LEVEL
```

Secret 참조가 필요한 경우 `envFrom[].secretRef` 또는 `valueFrom.secretKeyRef`를 사용한다.
참조 리소스 생성, 이미지 배포 및 실제 patch 적용은 별도 승인 작업이다. ConfigMap 데이터만
갱신한 경우도 새 값 적용에는 별도로 승인된 Pod 재시작이 필요하다.

## 수집 범위와 검증

실행 인자는 `-source=ebpf -output-mode=windows -export=tagcount -window=5s
-pod-identity=true`이며 종료 duration을 주지 않으므로 종료 신호까지 계속 수집한다.
DaemonSet이 종료된 컨테이너를 재시작하고 삭제된 Pod를 다시 생성한다.

- L4: TCP SRTT 집계. HTTP 요청 시간이나 application time이 아니다.
- HTTP: 현재 커널에서 관측 가능한 plaintext HTTP. HTTPS/OpenSSL은 대상 라이브러리를
  별도로 지정해야 하며, 이 기본 배포는 모든 TLS/HTTP2 트래픽의 수집을 보장하지 않는다.
- DNS: 관측 응답/무응답/불완전 결과와 측정된 지연. 실패 원인을 정책 차단으로 단정하지 않는다.
- Pod UID: 요청/query 관측 시점의 identity를 유지한다. 모르는 UID는 생략한다.
- coverage: 수집/상관관계 진단이다. 서비스 실패나 packet loss가 아니다.

`Ready`나 TCP write만으로 backend 저장을 증명할 수 없다. 실제 프로젝트의
`kube_network_edge_v1alpha1`, `kube_network_http_v1alpha1`,
`kube_network_dns_v1alpha1`, `kube_network_coverage_v1alpha1`을 조회하고
새 object name, 실제 시간 범위, UID 필드 존재 및 알려진 Pod UID와의 일치를 확인한다.
기존 Container Map 화면 연결은 별도 작업이다.

## 업데이트·비활성화·수동 배포에서 전환

image/resources/스케줄링/Secret 참조 변경은 network DaemonSet만 갱신한다.
`networkAgent.enabled: false`로 바꾸면 해당 CR이 소유한 network 리소스만 정리한다.
이 옵션 변경은 기존 node/master/operator 리소스나 외부 지정 SA/Secret을 정리하지 않는다.
**CR 전체 삭제는 다르다.** 기존 operator의 전체 agent 정리 경로도 실행하므로,
network collector만 끄려는 경우에는 CR을 삭제하지 않는다.

동일 이름의 수동 DaemonSet은 자동 인수하거나 삭제하지 않는다. 수동 배포에서 전환할 때:

1. 새 operator와 CRD 준비를 확인하되 networkAgent는 비활성화해 둔다.
2. 수동 DaemonSet의 소유자/UID/image와 rollback YAML을 확인한다.
3. 승인된 수동 DaemonSet만 제거하고 위 CR 블록을 활성화한다. 짧은 수집 공백이 발생한다.
4. 새 DaemonSet의 CR ownerReference, 한 노드 rollout, 실제 backend 저장을 확인한다.

동일 노드에 수동 collector와 operator collector를 중복 실행하지 않는다.
