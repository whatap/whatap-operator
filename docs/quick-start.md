## YAML 파일을 사용한 설치 (Helm 없이)

YAML 파일을 직접 사용하여 Whatap Operator를 설치할 수 있습니다:

1. Whatap Operator를 위한 네임스페이스를 생성합니다:

```bash
kubectl create namespace whatap-monitoring
```

빌드 플랫폼을 사용하여 kubectl 명령어를 사용할 수 없는 경우, resources.yaml 파일에 주석 처리된 네임스페이스 정의를 주석 해제하여 사용할 수 있습니다. 이 경우 별도로 네임스페이스를 생성할 필요가 없습니다.

2. Whatap 인증 정보를 담은 시크릿을 생성합니다:

```bash
kubectl create secret generic whatap-credentials -n whatap-monitoring \
  --from-literal=WHATAP_LICENSE="your-license-key" \
  --from-literal=WHATAP_HOST="your-whatap-server" \
  --from-literal=WHATAP_PORT="6600"
```

빌드 플랫폼을 사용하여 kubectl 명령어를 사용할 수 없는 경우, 다음과 같이 YAML 파일을 사용하여 시크릿을 생성할 수 있습니다:

```yaml
# whatap-credentials-secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: whatap-credentials
  namespace: whatap-monitoring
type: Opaque
stringData:
  WHATAP_LICENSE: "your-license-key"
  WHATAP_HOST: "your-whatap-server"
  WHATAP_PORT: "6600"
```

이 YAML 파일을 저장한 후 다음 명령으로 적용합니다:

```bash
kubectl apply -f whatap-credentials-secret.yaml
```

3. 배포 패키지에서 제공하는 압축 파일을 압축 해제합니다:

```bash
tar -xzf manifests.tar.gz
```

4. resources.yaml 파일을 편집하여 내부 레지스트리를 사용하도록 합니다:

```bash
# 기본 이미지를 내부 레지스트리 이미지로 교체
sed -i 's|${WHATAP_OPERATOR_IMAGE:-public.ecr.aws/whatap/whatap-operator:latest}|your-registry.example.com/whatap-operator:latest|g' manifests/resources.yaml
```

5. 파일을 적용합니다:

```bash
kubectl apply -f manifests/crd.yaml
kubectl apply -f manifests/resources.yaml
```


### 설치 확인

오퍼레이터가 올바르게 실행되고 있는지 확인하려면:

```bash
kubectl get pods -n whatap-monitoring
```

Whatap Operator 파드가 실행 중인 것을 확인할 수 있습니다.

## Whatap 모니터링 CR 구성

### 기본 쿠버네티스 모니터링

### 쿠버네티스 에이전트와 APM 설치

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  features:
    k8sAgent:
      # 폐쇄망 환경에서 내부 레지스트리의 kube_agent 이미지를 사용하려면 customAgentImageFullName 필드를 사용합니다
      customAgentImageFullName: "your-registry.example.com/your-image:latest"  #e.g)"your-registry.example.com/whatap_kube_agent:latest"
      masterAgent:
        enabled: true
      nodeAgent:
        enabled: true
    apm:
      instrumentation:
        targets:
          - name: java
            enabled: true
            language: "java"
            whatapApmVersions:
              java: latest
            # 폐쇄망 환경에서 내부 레지스트리의 이미지를 사용하려면 customImageName 필드를 사용합니다
            customImageName: "your-registry.example.com/whatap-apm-init-java:latest" #e.g)"your-registry.example.com/whatap-apm-init-java:latest"
            namespaceSelector:
              matchNames:
                - default
            podSelector:
              matchLabels:
                app: "java-app"
            config:
              mode: default
```

2. 구성을 적용합니다:

```bash
kubectl apply -f whatap-config.yaml
```


### 추가 설정 옵션

더 복잡한 설정이 필요한 경우 다음과 같은 추가 옵션을 사용할 수 있습니다:

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  features:
    k8sAgent:
      # 폐쇄망 환경에서 내부 레지스트리의 이미지를 사용하려면 customAgentImageFullName 필드를 사용합니다
      # customAgentImageFullName: "your-registry.example.com/whatap_kube_agent:latest"
      masterAgent:
        enabled: true
      nodeAgent:
        enabled: true
    apm:
      instrumentation:
        targets:
          - name: hello-world            # 대상 애플리케이션 이름
            enabled: true                # 이 대상에 대한 APM 활성화 여부
            language: "java"             # 지원 언어: java, python, php, dotnet, nodejs, golang
            whatapApmVersions:
              java: "2.2.58"             # 사용할 APM 에이전트 버전
            # 폐쇄망 환경에서 내부 레지스트리의 이미지를 사용하려면 customImageName 필드를 사용합니다
            # customImageName: "your-registry.example.com/apm-init-java:latest"

            # 대상 네임스페이스 선택
            namespaceSelector:
              matchNames:                # 특정 네임스페이스 이름으로 선택
                - default
              # matchLabels:             # 또는 네임스페이스 라벨로 선택
              #   environment: "production"

            # 대상 Pod 선택
            podSelector:
              matchLabels:               # Pod 라벨로 선택
                app: "hello-world"
              # matchExpressions:        # 또는 라벨 표현식으로 선택
              #   - key: "app"
              #     operator: "In"       # 지원 연산자: In, NotIn, Exists, DoesNotExist
              #     values:
              #       - "hello-world"

            # APM 에이전트 설정
            config:
              mode: default              # 기본 설정 사용 (default 또는 custom)
              # configMapRef:            # 커스텀 설정을 사용하는 경우 (mode: "custom"일 때)
              #   name: "apm-custom-config"    # 커스텀 설정이 포함된 ConfigMap 이름
              #   namespace: "default"         # ConfigMap이 위치한 네임스페이스
```

### APM 자동 계측에 대한 상세 설명

APM 자동 계측(Automatic Instrumentation)은 애플리케이션 코드를 수정하지 않고도 Whatap APM 에이전트를 주입하여 성능 모니터링을 가능하게 하는 기능입니다. 이 기능은 다음과 같이 작동합니다:

1. **작동 원리**: Whatap Operator는 지정된 네임스페이스와 라벨을 가진 Pod를 감시하고, 해당 Pod가 생성될 때 초기화 컨테이너(Init Container)를 통해 APM 에이전트를 주입합니다.

2. **대상 선택**: `namespaceSelector`와 `podSelector`를 사용하여 모니터링할 애플리케이션을 정확히 지정할 수 있습니다. 이를 통해 특정 네임스페이스나 라벨을 가진 Pod에만 선택적으로 에이전트를 주입할 수 있습니다.

3. **설정 관리**: `config.mode`를 통해 기본 설정(`default`) 또는 커스텀 설정(`custom`)을 사용할 수 있습니다. 커스텀 설정을 사용할 경우 ConfigMap을 통해 상세한 에이전트 설정을 제공할 수 있습니다.

### 중요 사항: 기존 애플리케이션에 대한 재시작 필요

**APM 자동 설치가 반영되려면 기존에 배포된 애플리케이션의 경우 rollout restart가 필요합니다:**

```bash
# 기존 애플리케이션 재시작 (예: Deployment)
kubectl rollout restart deployment/your-app-name -n your-namespace

# 또는 DaemonSet의 경우
kubectl rollout restart daemonset/your-app-name -n your-namespace

# 또는 StatefulSet의 경우
kubectl rollout restart statefulset/your-app-name -n your-namespace
```

**새로 배포하는 애플리케이션의 경우에는 따로 추가 설정이 필요하지 않습니다.** Whatap Operator가 자동으로 APM 에이전트를 주입합니다.
