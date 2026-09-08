와탭 오퍼레이터를 통한 모니터링 구조도

![image.png](/docs/src/img/architecture.png)

와탭 오퍼레이터는 쿠버네티스 환경에서 와탭 에이전트를 배포하고 구성할 수 있게 해주는 오픈소스 쿠버네티스 오퍼레이터입니다.

오퍼레이터를 활용하면 단일 커스텀 리소스(CRD)를 통해 K8s 및 GPU(MIG) 모니터링부터 자동 계측, 오픈메트릭 수집에 이르는 모든 기능을 통합 관리할 수 있습니다. 이 과정에서 오퍼레이터는 배포될 리소스의 유효성을 자동으로 검증하여, 복잡한 구성에서 발생할 수 있는 오류 가능성을 최소화하고 안정적인 운영을 지원합니다.

## 주요 특징

- **커스텀 리소스를 통한 배포**: 와탭 에이전트 및 관련 구성 요소를 쿠버네티스 커스텀 리소스를 활용하여 배포하고 관리합니다.
- **간소화된 배포 구성**: 와탭 에이전트의 버전, 리소스 요청/제한, 모니터링 대상 등록 등 필수 요소를 네이티브 쿠버네티스 리소스에서 간편하게 설정할 수 있습니다.
- **자동 APM 계측**: 쿠버네티스 표준 라벨 선택자를 이용하여 특정 파드에 APM 에이전트를 자동으로 주입합니다.
- **Open Agent를 통한 오픈메트릭 수집**: Open Agent 설치를 통해 오픈메트릭(OpenMetrics) 데이터를 수집하고 활용할 수 있습니다.
- **통합 모니터링 관리**: 단일 CR을 통해 애플리케이션 성능 모니터링(APM)과 쿠버네티스 인프라 모니터링을 한 번에 구성하고 관리하여 운영 효율성을 높입니다.

## **와탭 오퍼레이터의 목표**

와탭 오퍼레이터는 쿠버네티스 환경에서 모니터링 구성의 복잡성을 줄이고 관리를 단순화하는 데 중점을 둡니다.

- **에이전트 설치 및 구성 노력 감소**: 와탭 모니터링 에이전트의 설치와 관리 부담을 크게 줄여줍니다.
- **쿠버네티스 네이티브 리소스를 통한 자동화**: 쿠버네티스 CRD를 활용하여 와탭 모니터링 대상에 대한 설정을 자동으로 관리합니다.

    <aside>
    💡

  **예시:**

    ```yaml
    apiVersion: monitoring.whatap.com/v2alpha1
    kind: WhatapAgent
    metadata:
      name: whatap
    spec:
      features:
        apm:
          instrumentation:
            targets:
              - name: "java-app"
                enabled: true
                language: "java"
                whatapApmVersions:
                  java: "2.2.58"
                # 선택자 구성...
    
    ```

    </aside>

- **구성 추상화 및 유효성 검증**: 복잡한 모니터링 구성을 단순화하며, 표준 쿠버네티스 선택자(matchLabels, matchExpressions)를 지원합니다. 또한, 구성 유효성 검증을 통해 오류를 최소화하여 안정적인 운영을 돕습니다.

## 왜 헬름 차트나 수동 데몬셋 설치 대신 와탭 오퍼레이터를 사용해야 할까요?

헬름 차트나 데몬셋을 통해 와탭 에이전트를 설치할 수도 있지만, 와탭 오퍼레이터는 다음과 같은 장점을 제공합니다.

### **1. 자동 상태 조정**

헬름 차트나 데몬셋은 상태 변경 시 수동 개입이 필요하지만, 와탭 오퍼레이터는 쿠버네티스 **조정 루프(reconciliation loop)** 에 포함되어 CR의 상태를 지속적으로 감시하고 자동으로 조정합니다. 예를 들어, CR에 의해 생성된 리소스가 실수로 삭제되거나 변경되어도 오퍼레이터는 이를 감지하고 CR에 정의된 상태로 자동 복구합니다.

### **2. 구성 오류 최소화**

수동 설치 방식은 오류 발생 가능성이 높습니다. 와탭 오퍼레이터는 에이전트 구성에 대한 철저한 유효성 검증을 수행하여 이러한 오류를 최소화합니다.

### 3. 통합 관리

K8s 클러스터 내의 와탭 모니터링 컴포넌트 구성을 단일 파일로 통합 관리할 수 있어 운영 효율성이 향상됩니다.

### **4. 쿠버네티스 표준 준수**

오퍼레이터는 쿠버네티스 API의 일급 리소스로 취급되며, 쿠버네티스 표준 라벨 선택자를 완벽하게 지원합니다. 이를 통해 익숙한 쿠버네티스 패턴을 활용하여 모니터링 대상을 유연하게 선택할 수 있습니다.

[와탭 오퍼레이터 설치 가이드](./docs/operator.md)

## GPU 모니터링: kubelet pod-resources 경로 설정

기본 kubelet 경로가 아닌 환경에서는 기존 `WhatapAgent` CR의
`spec.features.k8sAgent.gpuMonitoring.podResourcesPath`에 **호스트의 pod-resources 디렉터리**를 지정합니다.
Operator `3.0.20` 이상과 이 필드를 포함한 CRD가 모두 필요합니다. Helm 설치는 차트 `1.9.11` 이상을
사용하고, 기존 `image.tag`를 재사용하는 업그레이드에서는 이미지도 `3.0.20` 이상으로 지정해야 합니다.
Operator `3.0.19`에는 지원되지 않습니다. 설치된 스키마는 다음 명령으로 확인합니다.

```bash
kubectl explain whatapagent.spec.features.k8sAgent.gpuMonitoring.podResourcesPath
```

예를 들어 kubelet 루트가 `/repo.p/kubelet`이고, GPU 노드에서
`/repo.p/kubelet/pod-resources/kubelet.sock` 소켓의 존재를 확인했다면 기존 CR에 다음 설정을 병합합니다.
아래는 전체 설치용 CR이 아닌 설정 부분 예시입니다.

```yaml
spec:
  features:
    k8sAgent:
      gpuMonitoring:
        enabled: true
        podResourcesPath: /repo.p/kubelet/pod-resources
```

- 미설정 또는 빈 문자열이면 기존 `/var/lib/kubelet/pod-resources`를 사용합니다.
- kubelet 루트(`/repo.p/kubelet`)나 소켓 파일(`kubelet.sock`)이 아닌, 소켓이 들어 있는 디렉터리의 절대 경로를 지정합니다.
- 호스트의 `pod-gpu-resources` 볼륨 경로만 바뀌며, exporter 내부에는 `/var/lib/kubelet/pod-resources`로 읽기 전용 마운트됩니다.
- `nodeAgent.runtimeSocketPath`는 별도의 컨테이너 런타임 소켓 설정이므로 이 옵션과 독립적입니다.
- 공통 Node Agent와 GPU 전용 DaemonSet 모두 적용됩니다. 대상 GPU 노드들은 같은 호스트 경로를 사용해야 합니다.
- DaemonSet을 직접 편집하거나 Operator를 중지할 필요 없이 CR을 변경하면 reconciliation으로 반영됩니다. 경로 변경 시 해당 DaemonSet의 Pod 템플릿이 갱신되어 롤링 업데이트가 발생합니다.
- 잘못된 경로를 자동 생성하지 않습니다. 실제 소켓과 노드 권한을 먼저 확인하고, 적용 후 DaemonSet의 `pod-gpu-resources.hostPath.path`, exporter 로그 및 GPU/Pod 매핑 지표를 확인합니다.

추가 문서
- [Helm차트에서 whatap-credentials Secret 생성 옵션 PRD](./docs/prd-helm-credentials.md)