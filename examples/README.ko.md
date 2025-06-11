# Whatap Operator 예제

이 디렉토리에는 Whatap Operator의 예제 구성이 포함되어 있습니다. 이 예제들은 Kubernetes 클러스터에서 Whatap 모니터링 솔루션을 구성하는 다양한 방법을 보여줍니다.

## WhatapAgent 예제

`whatapagent` 디렉토리에는 Whatap 모니터링 에이전트를 배포하고 구성하는 데 사용되는 `WhatapAgent` 커스텀 리소스의 예제 구성이 포함되어 있습니다.

### 기본 구성

[whatap-agent-basic.yaml](whatapagent/whatap-agent-basic.yaml) - 기본 Kubernetes 모니터링을 위해 Whatap 마스터 에이전트와 노드 에이전트를 활성화하는 최소 구성입니다.

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  license: "your-license-key"
  host: "whatap-server"
  port: "6600"
  features:
    k8sAgent:
      masterAgent:
        enabled: true
      nodeAgent:
        enabled: true
```

### 설명 주석이 포함된 템플릿

[whatap-agent-template.yaml](whatapagent/whatap-agent-template.yaml) - 모든 기능에 대한 주석 처리된 섹션과 각 옵션에 대한 자세한 설명 주석이 포함된 템플릿 구성입니다.

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  features:
    ### APM 자동 설치 사용시 주석 해제 - APM 에이전트를 애플리케이션 Pod에 자동으로 주입하여 애플리케이션 성능 모니터링을 활성화합니다.
    # apm:
    #   instrumentation:
    #     targets:
    #       - name: hello-world
    #         enabled: true
    #         language: "java"          # 지원 언어: java, python, php, dotnet, nodejs, golang
    #         whatapApmVersions:
    #           java: "2.2.58"          # 사용할 APM 에이전트 버전
    # ... (전체 주석이 포함된 템플릿은 파일을 참조하세요)
```

이 템플릿은 인라인 주석을 통해 각 구성 섹션의 목적을 이해하는 데 도움이 되도록 설계되었습니다. 필요한 섹션의 주석을 해제하고 요구 사항에 따라 사용자 정의하세요.

### APM 계측만 사용

[whatap-agent-apm-only.yaml](whatapagent/whatap-agent-apm-only.yaml) - Kubernetes 모니터링 에이전트를 활성화하지 않고 Java 애플리케이션에 대한 APM 계측을 구성합니다.

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  license: "your-license-key"
  host: "whatap-server"
  port: "6600"
  features:
    apm:
      instrumentation:
        targets:
          - name: hello-world
            enabled: true
            language: "java"
            whatapApmVersions:
              java: "2.2.58"
            namespaceSelector:
              matchNames:
                - default
            podSelector:
              matchLabels:
                app: "hello-world"
            config:
              mode: default
```

### OpenAgent만 사용

[whatap-agent-openagent-only.yaml](whatapagent/whatap-agent-openagent-only.yaml) - Kubernetes 모니터링 에이전트나 APM 계측을 활성화하지 않고 Prometheus 스타일 메트릭을 수집하기 위한 OpenAgent 컴포넌트만 구성합니다.

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  license: "your-license-key"
  host: "whatap-server"
  port: "6600"
  features:
    openAgent:
        enabled: true
        globalInterval: "60s"
        globalPath: "/metrics"
        targets:
          - targetName: kube-apiserver
            type: ServiceMonitor
            namespaceSelector:
              matchNames:
                - "default"
            selector:
              matchLabels:
                component: apiserver
                provider: kubernetes
            endpoints:
              - port: "https"
                path: "/metrics"
                interval: "30s"
                scheme: "https"
                tlsConfig:
                  insecureSkipVerify: true
            metricRelabelConfigs:
              - source_labels: ["__name__"]
                regex: "apiserver_request_total"
                action: "keep"
```

### Kubernetes 모니터링과 APM 계측 함께 사용

[whatap-agent-k8s-apm.yaml](whatapagent/whatap-agent-k8s-apm.yaml) - Kubernetes 모니터링과 Java 애플리케이션에 대한 APM 계측을 결합합니다.

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  license: "your-license-key"
  host: "whatap-server"
  port: "6600"
  features:
    apm:
      instrumentation:
        targets:
          - name: hello-world
            enabled: true
            language: "java"
            whatapApmVersions:
              java: "2.2.58"
            namespaceSelector:
              matchNames:
                - default
            podSelector:
              matchLabels:
                app: "hello-world"
            config:
              mode: default
    k8sAgent:
      masterAgent:
        enabled: true
      nodeAgent:
        enabled: true
```

### 전체 구성

[whatap-agent-complete.yaml](whatapagent/whatap-agent-complete.yaml) - Kubernetes 모니터링, APM 계측 및 OpenMetric 수집을 활성화하는 포괄적인 구성입니다.

```yaml
# 전체 예제는 파일을 참조하세요
```

### 커스텀 리소스 요구사항이 있는 K8s 에이전트

[whatap-agent-k8s-resources.yaml](whatapagent/whatap-agent-k8s-resources.yaml) - Whatap 마스터 에이전트와 노드 에이전트에 대한 커스텀 리소스 요구사항(CPU 및 메모리)을 구성합니다.

#### Kubernetes 리소스 요구사항 이해하기

Kubernetes에서 리소스 요구사항은 스케줄러가 파드를 배치할 노드를 결정하고 파드가 효과적으로 실행되는 데 필요한 리소스를 확보하는 데 도움을 줍니다:

- **요청(Requests)**: 컨테이너가 필요로 하는 최소한의 리소스 양입니다. 스케줄러는 이를 사용하여 충분한 리소스가 있는 노드를 찾습니다.
- **제한(Limits)**: 컨테이너가 사용할 수 있는 최대 리소스 양입니다. 이는 컨테이너가 노드에서 공정한 몫 이상의 리소스를 사용하는 것을 방지합니다.

적절한 리소스 요구사항을 설정하는 것은 다음과 같은 이유로 중요합니다:
- 모니터링 에이전트의 안정적인 성능 보장
- 다른 워크로드와의 리소스 경합 방지
- 클러스터 전체의 리소스 활용 최적화

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  license: "your-license-key"
  host: "whatap-server"
  port: "6600"
  features:
    k8sAgent:
      masterAgent:
        enabled: true
        resources:
          requests:
            cpu: "200m"        # 200 밀리코어(0.2 CPU 코어) 요청
            memory: "400Mi"    # 400 MiB의 메모리 요청
          limits:
            cpu: "500m"        # 500 밀리코어(0.5 CPU 코어)로 제한
            memory: "600Mi"    # 600 MiB의 메모리로 제한
      nodeAgent:
        enabled: true
        resources:
          requests:
            cpu: "150m"        # 150 밀리코어(0.15 CPU 코어) 요청
            memory: "350Mi"    # 350 MiB의 메모리 요청
          limits:
            cpu: "300m"        # 300 밀리코어(0.3 CPU 코어)로 제한
            memory: "500Mi"    # 500 MiB의 메모리로 제한
```

리소스 요구사항을 지정하지 않으면 Whatap Operator는 에이전트가 제대로 작동하는 데 충분한 리소스를 확보할 수 있도록 기본값을 적용합니다. 기본값은 다음과 같습니다:

- 마스터 에이전트:
  - 요청(Requests): CPU: 100m, 메모리: 300Mi
  - 제한(Limits): CPU: 200m, 메모리: 350Mi

- 노드 에이전트:
  - 요청(Requests): CPU: 100m, 메모리: 300Mi
  - 제한(Limits): CPU: 200m, 메모리: 350Mi

### 커스텀 톨러레이션이 있는 K8s 에이전트

[whatap-agent-k8s-tolerations.yaml](whatapagent/whatap-agent-k8s-tolerations.yaml) - Whatap 에이전트에 커스텀 톨러레이션을 추가하는 방법을 보여줍니다.

#### Kubernetes 톨러레이션 이해하기

Kubernetes에서 **테인트(taints)**와 **톨러레이션(tolerations)**은 함께 작동하여 파드가 부적절한 노드에 스케줄링되지 않도록 합니다:

- **테인트**는 노드에 적용되어 특정 파드를 거부할 수 있게 합니다.
- **톨러레이션**은 파드에 적용되어 일치하는 테인트가 있는 노드에 파드가 스케줄링될 수 있게 합니다(필수는 아님).

톨러레이션은 모니터링 에이전트에 다음과 같은 이유로 중요합니다:
- 특별한 테인트가 있는 노드를 포함한 모든 노드에서 모니터링 범위를 보장합니다
- 특수 워크로드(예: GPU 노드, 전용 노드)의 모니터링을 가능하게 합니다
- 노드 유지 관리나 문제 발생 시에도 모니터링을 유지할 수 있습니다

각 톨러레이션은 다음으로 구성됩니다:
- **key**: 일치시킬 테인트 키
- **operator**: `Equal`(키와 값이 모두 일치해야 함) 또는 `Exists`(키만 일치하면 됨)
- **value**: 일치시킬 테인트 값(`Equal` 연산자에서만 사용)
- **effect**: 테인트를 허용하지 않는 파드에 어떤 일이 발생하는지(`NoSchedule`, `PreferNoSchedule`, 또는 `NoExecute`)

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  license: "your-license-key"
  host: "whatap-server"
  port: "6600"
  features:
    k8sAgent:
      masterAgent:
        enabled: true
        # 마스터 에이전트에 대한 톨러레이션
        tolerations:
          - key: "dedicated"            # "dedicated" 테인트가 있는 노드와 일치
            operator: "Equal"           # 키와 값이 모두 일치해야 함
            value: "monitoring"         # 일치시킬 테인트 값
            effect: "NoSchedule"        # NoSchedule 효과 허용
          - key: "special-workload"     # "special-workload" 테인트가 있는 노드와 일치
            operator: "Exists"          # 이 키에 대한 모든 값과 일치
            effect: "NoSchedule"        # NoSchedule 효과 허용
      nodeAgent:
        enabled: true
        # 노드 에이전트에 대한 톨러레이션
        # 참고: 이는 마스터 및 컨트롤 플레인 노드에 대한 기본 톨러레이션에 추가됩니다
        tolerations:
          - key: "dedicated"            # "dedicated" 테인트가 있는 노드와 일치
            operator: "Equal"           # 키와 값이 모두 일치해야 함
            value: "monitoring"         # 일치시킬 테인트 값
            effect: "NoSchedule"        # NoSchedule 효과 허용
          - key: "gpu"                  # "gpu" 테인트가 있는 노드와 일치
            operator: "Exists"          # 이 키에 대한 모든 값과 일치
            effect: "NoSchedule"        # NoSchedule 효과 허용
```

#### Whatap Operator에서 톨러레이션 작동 방식

WhatapAgent CR은 마스터 에이전트와 노드 에이전트 모두에 대한 톨러레이션을 직접 지정할 수 있습니다. CR에 지정된 톨러레이션은 각 파드에 적용됩니다.

노드 에이전트의 경우, 지정된 톨러레이션은 마스터 및 컨트롤 플레인 노드에 대한 기본 톨러레이션에 추가됩니다:
- `key: "node-role.kubernetes.io/master", effect: "NoSchedule"`
- `key: "node-role.kubernetes.io/control-plane", effect: "NoSchedule"`

이를 통해 노드 에이전트가 마스터/컨트롤 플레인 노드 및 커스텀 테인트가 있는 노드를 포함한 모든 노드에서 실행되도록 보장합니다.

#### 커스텀 톨러레이션의 일반적인 사용 사례

1. **전용 노드 모니터링**: 특정 워크로드 전용 노드가 있는 경우(예: `dedicated=workload-type` 테인트가 있는 노드), 모니터링 범위를 보장하기 위해 일치하는 톨러레이션을 추가합니다.

2. **GPU 노드**: GPU가 있는 노드는 종종 GPU 워크로드만 실행되도록 특별한 테인트를 가집니다. 이러한 특수 노드를 모니터링하기 위해 톨러레이션을 추가합니다.

3. **프로덕션 vs. 개발**: 테인트를 사용하여 프로덕션 및 개발 워크로드를 분리하는 경우, 모니터링 에이전트가 두 환경 모두에서 실행될 수 있도록 합니다.

4. **노드 유지 관리**: 노드가 코든(cordon)되거나 유지 관리를 위해 표시된 경우, 적절한 톨러레이션이 있는 모니터링 에이전트는 계속 실행되어 유지 관리 과정 중에도 가시성을 제공할 수 있습니다.

### 시크릿 기반 구성

[whatap-agent-secret.yaml](whatapagent/whatap-agent-secret.yaml) - CR에 직접 지정하는 대신 Kubernetes 시크릿을 사용하여 Whatap 자격 증명을 저장합니다.

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  # 여기에 license, host, port가 지정되지 않음
  # 이 값들은 "whatap-credentials" 시크릿에서 가져옴
  features:
    k8sAgent:
      masterAgent:
        enabled: true
      nodeAgent:
        enabled: true
```

## 사용법

### 직접 구성 사용

CR에 자격 증명을 직접 포함하는 예제 구성을 적용하려면 다음 명령을 사용하세요:

```bash
kubectl apply -f https://raw.githubusercontent.com/whatap/whatap-operator/main/examples/whatapagent/whatap-agent-basic.yaml
```

사용하려는 특정 예제로 URL을 대체하세요.

구성을 적용하기 전에 플레이스홀더 값(`your-license-key`, `whatap-server` 등)을 실제 Whatap 자격 증명으로 대체해야 합니다.

### 시크릿 기반 구성 사용

시크릿 기반 접근 방식을 사용하려면 먼저 Whatap 자격 증명으로 시크릿을 생성하세요:

```bash
kubectl create secret generic whatap-credentials --namespace whatap-monitoring \
  --from-literal=license=$WHATAP_LICENSE \
  --from-literal=host=$WHATAP_HOST \
  --from-literal=port=$WHATAP_PORT
```

그런 다음 시크릿을 사용하는 구성을 적용하세요:

```bash
kubectl apply -f https://raw.githubusercontent.com/whatap/whatap-operator/main/examples/whatapagent/whatap-agent-secret.yaml
```
