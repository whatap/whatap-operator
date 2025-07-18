# 와탭 오퍼레이터(Whatap Operator)
와탭 오퍼레이터는 쿠버네티스 환경에서 와탭 에이전트를 배포하고 구성할 수 있게 해주는 오픈소스 쿠버네티스 오퍼레이터입니다.
오퍼레이터를 활용하면 단일 커스텀 리소스(CRD)를 통해 K8s 및 GPU(MIG) 모니터링부터 자동 계측, 오픈메트릭 수집에 이르는 모든 기능을 통합 관리할 수 있습니다. 이 과정에서 오퍼레이터는 배포될 리소스의 유효성을 자동으로 검증하여, 복잡한 구성에서 발생할 수 있는 오류 가능성을 최소화하고 안정적인 운영을 지원합니다.

## 주요 특징

- **커스텀 리소스를 통한 배포**: 와탭 에이전트 및 관련 구성 요소를 쿠버네티스 커스텀 리소스를 활용하여 배포하고 관리합니다.
- **간소화된 배포 구성**: 와탭 에이전트의 버전, 리소스 요청/제한, 모니터링 대상 등록 등 필수 요소를 네이티브 쿠버네티스 리소스에서 간편하게 설정할 수 있습니다.
- **자동 APM 계측**: 쿠버네티스 표준 라벨 선택자를 이용하여 특정 파드에 APM 에이전트를 자동으로 주입합니다.
- **Open Agent를 통한 오픈메트릭 수집**: Open Agent 설치를 통해 오픈메트릭(OpenMetrics) 데이터를 수집하고 활용할 수 있습니다.
- **통합 모니터링 관리**: 단일 CR을 통해 애플리케이션 성능 모니터링(APM)과 쿠버네티스 인프라 모니터링을 한 번에 구성하고 관리하여 운영 효율성을 높입니다.


![architecture.png](docs/src/img/architecture.png)

## **와탭 오퍼레이터의 목표**

와탭 오퍼레이터는 쿠버네티스 환경에서 모니터링 구성의 복잡성을 줄이고 관리를 단순화하는 데 중점을 둡니다.

- **에이전트 설치 및 구성 노력 감소**: 와탭 모니터링 에이전트의 설치와 관리 부담을 크게 줄여줍니다.
- **쿠버네티스 네이티브 리소스를 통한 자동화**: 쿠버네티스 CRD를 활용하여 와탭 모니터링 대상에 대한 설정을 자동으로 관리합니다.
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
- **구성 추상화 및 유효성 검증**: 복잡한 모니터링 구성을 단순화하며, 표준 쿠버네티스 선택자(matchLabels, matchExpressions)를 지원합니다. 또한, 구성 유효성 검증을 통해 오류를 최소화하여 안정적인 운영을 돕습니다.


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

### 오퍼레이터 예제 구성

[whatap-agent-operator-example.yaml](whatapagent/whatap-agent-operator-example.yaml) - Whatap 오퍼레이터와 함께 사용하기 위한 실용적인 예제 구성으로, APM, K8sAgent 및 OpenAgent를 포함하며 한국어 주석이 있습니다.

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  features:
    ### APM 설정 - 애플리케이션 성능 모니터링을 위한 에이전트 자동 주입
    apm:
      instrumentation:
        targets:
          - name: "sample-app"
            enabled: true
            language: "java"
            whatapApmVersions:
              java: "latest"
            # ... (전체 예제는 파일을 참조하세요)
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

### K8s 에이전트에 사용자 정의 환경 변수 추가

[whatap-agent-k8s-envs.yaml](whatapagent/whatap-agent-k8s-envs.yaml) - Whatap 에이전트에 사용자 정의 환경 변수를 추가하는 방법을 보여줍니다.

### K8s 에이전트의 컨테이너별 구성

[whatap-agent-k8s-container-config.yaml](whatapagent/whatap-agent-k8s-container-config.yaml) - whatap-node-agent와 whatap-node-helper 컨테이너를 별도로 구성하는 방법을 보여줍니다.

#### 컨테이너별 구성 이해하기

NodeAgent 데몬셋은 두 개의 컨테이너로 구성됩니다:
- **whatap-node-agent**: 노드 메트릭을 수집하는 메인 컨테이너
- **whatap-node-helper**: 컨테이너 메트릭 수집을 지원하는 헬퍼 컨테이너

NodeAgent 스펙에서 `nodeAgentContainer`와 `nodeHelperContainer` 필드를 사용하여 이러한 컨테이너를 별도로 구성할 수 있습니다. 이를 통해 다음과 같은 작업이 가능합니다:

- 각 컨테이너에 대해 다른 리소스 요구 사항 설정
- 각 컨테이너에 대해 다른 환경 변수 구성
- 각 컨테이너의 특정 역할에 따라 최적화

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  features:
    k8sAgent:
      nodeAgent:
        enabled: true
        # whatap-node-agent 컨테이너에 대한 특정 구성
        nodeAgentContainer:
          resources:
            requests:
              cpu: "150m"
              memory: "350Mi"
            limits:
              cpu: "300m"
              memory: "500Mi"
          envs:
            - name: NODE_AGENT_CUSTOM_ENV
              value: "custom-value"

        # whatap-node-helper 컨테이너에 대한 특정 구성
        nodeHelperContainer:
          resources:
            requests:
              cpu: "100m"
              memory: "150Mi"
            limits:
              cpu: "200m"
              memory: "300Mi"
          envs:
            - name: NODE_HELPER_CUSTOM_ENV
              value: "helper-value"
```

이 기능은 다음과 같은 경우에 특히 유용합니다:
1. 두 컨테이너 간의 리소스 할당을 미세 조정해야 하는 경우
2. 한 컨테이너에는 특정 환경 변수를 설정하고 다른 컨테이너에는 설정하지 않아야 하는 경우
3. 각 컨테이너의 워크로드에 따라 리소스를 할당하여 성능을 최적화하려는 경우

#### Kubernetes에서 환경 변수 이해하기

Kubernetes 파드의 환경 변수는 컨테이너에서 실행되는 애플리케이션에 구성을 전달하는 방법을 제공합니다. 모니터링 에이전트의 경우 환경 변수를 다음과 같은 용도로 사용할 수 있습니다:

- 에이전트 동작 및 기능 구성
- 모니터링 매개변수 및 임계값 설정
- 외부 서비스 또는 데이터 소스에 연결
- 특정 모니터링 기능 활성화 또는 비활성화

Whatap Operator는 WhatapAgent CR을 통해 마스터 에이전트와 노드 에이전트 모두에 대해 사용자 정의 환경 변수를 지정할 수 있도록 합니다.

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  features:
    k8sAgent:
      masterAgent:
        enabled: true
        # 마스터 에이전트용 사용자 정의 환경 변수
        envs:
          - name: CUSTOM_ENV_VAR1
            value: "value1"
          - name: CUSTOM_ENV_VAR2
            value: "value2"
      nodeAgent:
        enabled: true
        # 노드 에이전트용 사용자 정의 환경 변수
        envs:
          - name: NODE_CUSTOM_ENV_VAR1
            value: "node_value1"
          - name: NODE_CUSTOM_ENV_VAR2
            value: "node_value2"
          # ConfigMap에서 환경 변수 가져오기
          - name: CONFIG_ENV_VAR
            valueFrom:
              configMapKeyRef:
                name: my-config-map
                key: config-key
          # Secret에서 환경 변수 가져오기
          - name: SECRET_ENV_VAR
            valueFrom:
              secretKeyRef:
                name: my-secret
                key: secret-key
```

#### 환경 변수 소스 유형

환경 변수는 여러 가지 방법으로 지정할 수 있습니다:

1. **직접 값**: CR에서 직접 값 설정
   ```yaml
   - name: ENV_NAME
     value: "env_value"
   ```

2. **ConfigMap에서**: ConfigMap의 값 참조
   ```yaml
   - name: ENV_NAME
     valueFrom:
       configMapKeyRef:
         name: my-config-map
         key: config-key
   ```

3. **Secret에서**: Secret의 값 참조
   ```yaml
   - name: ENV_NAME
     valueFrom:
       secretKeyRef:
         name: my-secret
         key: secret-key
   ```

4. **필드에서**: 파드 또는 컨테이너의 필드 참조
   ```yaml
   - name: NODE_NAME
     valueFrom:
       fieldRef:
         fieldPath: spec.nodeName
   ```

#### 사용자 정의 환경 변수의 일반적인 사용 사례

1. **에이전트 구성**: 에이전트별 구성 매개변수 설정
2. **프록시 설정**: 아웃바운드 연결을 위한 프록시 구성
3. **디버그 수준**: 문제 해결을 위한 로깅 또는 디버그 수준 설정
4. **기능 플래그**: 특정 모니터링 기능 활성화 또는 비활성화
5. **통합 설정**: 다른 시스템과의 통합 구성

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
kubectl create secret generic whatap-credentials --namespace whatap-monitoring --from-literal=WHATAP_LICENSE=$WHATAP_LICENSE --from-literal=WHATAP_HOST=$WHATAP_HOST --from-literal=WHATAP_PORT=$WHATAP_PORT
```

그런 다음 시크릿을 사용하는 구성을 적용하세요:

```bash
kubectl apply -f https://raw.githubusercontent.com/whatap/whatap-operator/main/examples/whatapagent/whatap-agent-secret.yaml
```
