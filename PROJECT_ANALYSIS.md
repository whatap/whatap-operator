# Whatap-Operator 프로젝트 분석 문서

## 1. 프로젝트 개요

**whatap-operator**는 Kubebuilder v4로 구축된 Kubernetes Operator로, Whatap APM(Application Performance Monitoring) 에이전트의 배포 및 계측을 Kubernetes 클러스터 전체에서 관리합니다.

### 핵심 기술 스택
| 항목 | 버전/값 |
|------|---------|
| Go | 1.24.0+ |
| Kubernetes API | 0.33.0 |
| controller-runtime | v0.20.4 |
| API Domain | `monitoring.whatap.com` |
| API Version | `v2alpha1` (Alpha) |

---

## 2. 프로젝트 구조

```
whatap-operator/
├── api/v2alpha1/                          # API 타입 정의 (CRD)
│   ├── whatapagent_types.go              # 메인 WhatapAgent CRD (593줄)
│   ├── whatapservicemonitor_types.go     # 서비스 모니터링 CRD
│   ├── whatappodmonitor_types.go         # 파드 모니터링 CRD
│   ├── groupversion_info.go              # API 그룹/버전 등록
│   └── zz_generated.deepcopy.go          # 자동 생성된 deepcopy 메서드
│
├── internal/                              # 내부 구현
│   ├── controller/                        # 컨트롤러 재조정 로직
│   │   ├── whatapagent_controller.go     # 메인 Reconciler (712줄)
│   │   ├── install_agents.go             # 에이전트 설치 (2,343줄)
│   │   ├── gpu_mem_check.go              # GPU 메모리 모니터링
│   │   └── *_test.go                     # 단위 테스트
│   │
│   ├── webhook/v2alpha1/                 # 웹훅 핸들러 & APM 주입
│   │   ├── whatapagent_webhook.go        # 웹훅 등록 & 검증
│   │   ├── injector_java.go              # Java APM 주입 로직
│   │   ├── injector_python.go            # Python APM 주입 로직
│   │   ├── injector_nodejs.go            # Node.js APM 주입 로직
│   │   ├── process_deployments.go        # Init 컨테이너 생성
│   │   ├── utils.go                      # 공통 유틸리티
│   │   └── constants.go                  # 환경 변수 & 상수
│   │
│   ├── config/                           # 설정 관리
│   │   └── env.go                        # 환경 변수 처리
│   │
│   └── gpu/                              # GPU 모니터링 지원
│       └── csv.go                        # NVIDIA DCGM 메트릭 정의
│
├── cmd/
│   └── main.go                          # Operator 진입점 (350줄)
│
├── config/                               # Kubernetes 매니페스트 & 설정
│   ├── crd/bases/                       # 생성된 CRD YAML 파일
│   ├── rbac/                            # RBAC 설정
│   ├── webhook/                         # 웹훅 서버 설정
│   ├── manager/                         # Operator 매니저 배포
│   ├── default/                         # 기본 kustomization
│   ├── certmanager/                     # 인증서 관리
│   ├── prometheus/                      # Prometheus 모니터링
│   ├── network-policy/                  # 네트워크 정책
│   └── samples/                         # 샘플 CR
│
├── examples/                            # 예제 설정 파일 (14개 이상)
├── test/                                # 테스트 스위트
├── Dockerfile                           # 멀티 스테이지 빌드
├── Makefile                            # 빌드 자동화 (450줄 이상)
├── go.mod / go.sum                     # Go 의존성
└── PROJECT                             # Kubebuilder 프로젝트 설정
```

---

## 3. 진입점 및 애플리케이션 흐름

**파일:** `cmd/main.go` (350줄)

### 주요 함수
1. **generateSelfSignedCert()** - 웹훅 TLS용 CA 및 서버 인증서 생성
2. **main()** - Operator 초기화

### 실행 흐름
```
main()
  ├─ 커맨드라인 플래그 파싱 (metrics-bind-address, leader-elect 등)
  ├─ 로깅 설정 (Zap, 선택적 개발 모드)
  ├─ 웹훅용 자체 서명 인증서 생성
  │  └─ CA 번들, 서버 cert/key를 /etc/webhook/certs에 생성
  ├─ 웹훅 서버 초기화
  │  └─ 포트 9443, TLS 적용
  ├─ 컨트롤러 매니저 생성
  │  ├─ 메트릭 서버 (HTTPS/HTTP 설정 가능)
  │  ├─ 웹훅 서버
  │  └─ 리더 선출 지원
  ├─ 컨트롤러 스킴 등록
  ├─ WhatapAgentReconciler 설정
  ├─ 웹훅 설정 (ENABLE_WEBHOOKS != "false"인 경우)
  ├─ 헬스 프로브 추가 (/healthz, /readyz)
  ├─ GPU 메모리 체커 추가 (선택적)
  └─ 매니저 시작 (Ctrl-C까지 블로킹)
```

### 환경 변수
| 변수명 | 설명 |
|--------|------|
| `WHATAP_LICENSE` | Whatap 모니터링 라이선스 |
| `WHATAP_HOST` | Whatap 서버 주소 |
| `WHATAP_PORT` | Whatap 서버 포트 |
| `WHATAP_DEFAULT_NAMESPACE` | 기본 네임스페이스 (기본값: "whatap-monitoring") |
| `ENABLE_WEBHOOKS` | 웹훅 활성화/비활성화 (기본값: true) |
| `DEBUG` / `debug` | 개발 로깅 모드 활성화 |
| `ENABLED_WHATAP_DCGM_EXPORTER_MEMORY_CHECK` | GPU 메모리 모니터링 |

---

## 4. Custom Resource Definitions (CRDs)

### 4.1 WhatapAgent (메인 CRD)

**파일:** `api/v2alpha1/whatapagent_types.go` (593줄)

**메타데이터:**
- API Group: `monitoring.whatap.com`
- Version: `v2alpha1`
- Kind: `WhatapAgent`
- Scope: **Cluster** (네임스페이스 범위 아님)
- 서브리소스: `/status`

**Spec 구조:**

```yaml
WhatapAgentSpec:
  license: string              # Whatap 라이선스 (선택적)
  host: string                # Whatap 서버 호스트
  port: string                # Whatap 서버 포트

  features:
    apm:                       # Application Performance Monitoring
      instrumentation:
        enabled: bool          # APM 주입 활성화 (기본값: true)
        initContainerSecurity: # init 컨테이너 보안 컨텍스트
          runAsNonRoot: *bool
          runAsUser: *int64
        targets:              # APM 주입 대상
          - name: string
            enabled: bool
            language: java|python|php|dotnet|nodejs|golang
            whatapApmVersions: map[string]string
            customImageFullName: string
            additionalArgs: map[string]string
            envs: []EnvVar
            namespaceSelector:
              matchNames: []string
              matchLabels: map[string]string
              matchExpressions: []LabelSelectorRequirement
            podSelector:
              matchLabels: map[string]string
              matchExpressions: []LabelSelectorRequirement
            config:
              mode: default|custom
              configMapRef: {name: string}
            initContainerSecurity: {...}
            imagePullSecrets: []LocalObjectReference

    openAgent:                 # Open Telemetry Agent
      enabled: bool
      imageName: string
      imageVersion: string
      customImageFullName: string
      labels: map[string]string
      annotations: map[string]string
      podLabels: map[string]string
      podAnnotations: map[string]string
      imagePullSecrets: []LocalObjectReference
      priorityClassName: string
      nodeSelector: map[string]string
      affinity: *Affinity
      tolerations: []Toleration
      nodeName: string
      podSecurityContext: *PodSecurityContext
      envs: []EnvVar
      disableForeground: bool
      targets: []OpenAgentTarget

    k8sAgent:                  # Kubernetes 인프라 에이전트
      namespace: string        # 에이전트 네임스페이스
      agentImageName: string
      agentImageVersion: string
      customImageFullName: string
      imagePullSecrets: []LocalObjectReference

      masterAgent:             # Deployment
        enabled: bool
        resources: ResourceRequirements
        envs: []EnvVar
        tolerations: []Toleration
        affinity: *Affinity
        nodeSelector: map[string]string
        imagePullSecrets: []LocalObjectReference
        priorityClassName: string
        labels/annotations/podLabels/podAnnotations: map[string]string
        podSecurityContext: *PodSecurityContext
        runtimeClassName: string
        hostPID: bool
        masterAgentContainer:
          image: string
          resources: ResourceRequirements
          envs: []EnvVar
          securityContext: *SecurityContext

      nodeAgent:               # DaemonSet
        enabled: bool
        resources: ResourceRequirements
        envs: []EnvVar
        tolerations: []Toleration
        affinity: *Affinity
        nodeSelector: map[string]string
        imagePullSecrets: []LocalObjectReference
        priorityClassName: string
        labels/annotations/podLabels/podAnnotations: map[string]string
        podSecurityContext: *PodSecurityContext
        nodeAgentContainer: ContainerSpec
        nodeHelperContainer: ContainerSpec
        runtime: containerd|docker|crio (기본값: containerd)
        runtimeSocketPath: string
        hostNetwork: *bool (기본값: true)
        runtimeClassName: string
        hostPID: bool

      gpuMonitoring:           # GPU 모니터링
        enabled: bool
        customImageFullName: string
        service:
          enabled: bool
          type: ClusterIP|NodePort|LoadBalancer
          nodePort: int32
          port: int32 (기본값: 9400)
        envs: []EnvVar

      apiserverMonitoring: AgentComponentSpec
      etcdMonitoring: AgentComponentSpec
      schedulerMonitoring: AgentComponentSpec
```

### 4.2 WhatapServiceMonitor

**파일:** `api/v2alpha1/whatapservicemonitor_types.go` (74줄)

**목적:** OpenAgent가 메트릭을 스크레이핑할 서비스 정의

```yaml
Spec:
  selector: LabelSelector     # 모니터링할 서비스
  namespaceSelector: NamespaceSelector
  endpoints: []OpenAgentEndpoint
  relabelConfigs: []MetricRelabelConfig
  jobLabel: string
```

### 4.3 WhatapPodMonitor

**파일:** `api/v2alpha1/whatappodmonitor_types.go` (74줄)

**목적:** OpenAgent가 메트릭을 스크레이핑할 파드 정의

```yaml
Spec:
  selector: LabelSelector     # 모니터링할 파드
  namespaceSelector: NamespaceSelector
  endpoints: []OpenAgentEndpoint
  relabelConfigs: []MetricRelabelConfig
  jobLabel: string
```

---

## 5. 컨트롤러 및 재조정 로직

### 5.1 WhatapAgentReconciler

**파일:** `internal/controller/whatapagent_controller.go` (712줄)

**구조체:**
```go
type WhatapAgentReconciler struct {
    client.Client                    // Kubernetes 클라이언트
    Scheme           *runtime.Scheme  // 타입 스킴
    Recorder         record.EventRecorder  // 이벤트 레코더
    DefaultNamespace string           // Whatap 에이전트 네임스페이스
    WebhookCABundle  []byte          // 웹훅용 CA 인증서
    CaKey            []byte          // CA 개인 키
    ServerCert       []byte          // 서버 인증서
    ServerKey        []byte          // 서버 개인 키
}
```

**주요 메서드:**

| 메서드 | 설명 |
|--------|------|
| `Reconcile()` | 메인 재조정 루프 |
| `ensureWebhookService()` | 웹훅 서비스 생성 |
| `ensureWebhookTLSSecret()` | 웹훅 TLS 시크릿 생성 |
| `ensureMutatingWebhookConfiguration()` | 웹훅 규칙 등록 |
| `cleanupMasterAgent()` | Master Agent Deployment 삭제 |
| `cleanupNodeAgent()` | Node Agent DaemonSet 삭제 |
| `cleanupOpenAgent()` | OpenAgent 리소스 삭제 |
| `cleanupAgents()` | CR 삭제 시 전체 정리 |
| `SetupWithManager()` | 매니저에 등록 |
| `findWhatapAgents()` | 크로스 리소스 워치용 맵 함수 |

**RBAC 권한:**
```yaml
- monitoring.whatap.com/whatapagents: get, list, watch, create, update, patch, delete
- monitoring.whatap.com/whatapagents/status: get, update, patch
- monitoring.whatap.com/whatapagents/finalizers: update
- monitoring.whatap.com/whatappodmonitors, whatapservicemonitors: get, list, watch
- apps/deployments, daemonsets: all
- core/services, configmaps, secrets, serviceaccounts: all
- rbac/clusterroles, clusterrolebindings: all
- admissionregistration/mutatingwebhookconfigurations: all
```

### 5.2 에이전트 설치 로직

**파일:** `internal/controller/install_agents.go` (2,343줄)

**주요 함수:**

| 함수 | 설명 |
|------|------|
| `createOrUpdateMasterAgent()` | Master Agent Deployment 생성 |
| `getMasterAgentDeploymentSpec()` | DeploymentSpec 생성 |
| `createOrUpdateNodeAgent()` | Node Agent DaemonSet 생성 |
| `getNodeAgentDaemonSetSpec()` | DaemonSetSpec 생성 |
| `createOrUpdateGpuConfigMap()` | NVIDIA DCGM 메트릭 ConfigMap 생성 |
| `addDcgmExporterToNodeAgent()` | DCGM exporter 사이드카 추가 |
| `ensureDcgmExporterService()` | DCGM exporter 메트릭 서비스 생성 |

### 5.3 GPU 메모리 체커

**파일:** `internal/controller/gpu_mem_check.go`

**기능:**
- 별도 고루틴으로 실행
- 설정 가능한 체크 간격 (기본값: 30초)
- dcgm-exporter 파드 메모리 모니터링
- 임계값 초과 시 자동 재시작

---

## 6. 웹훅 시스템 및 APM 주입

### 6.1 웹훅 등록

**파일:** `internal/webhook/v2alpha1/whatapagent_webhook.go`

**두 개의 웹훅 등록:**

| 웹훅 | 경로 | 동작 |
|------|------|------|
| Pod Mutation | `/whatap-injection--v1-pod` | APM init 컨테이너 & 환경 변수 주입 |
| WhatapAgent Validation | `/whatap-validation--v2alpha1-whatapagent` | CR 설정 검증 |

### 6.2 APM 주입 로직

**파일:** `internal/webhook/v2alpha1/process_deployments.go`

**프로세스:**
1. 파드 생성 인터셉트
2. APM instrumentation 활성화 여부 확인
3. 대상 기준(네임스페이스, 레이블)과 파드 매칭
4. 각 매칭 대상에 대해:
   - APM 에이전트를 포함한 init 컨테이너 생성
   - 파드 스펙에 init 컨테이너 주입
   - 앱 컨테이너에 환경 변수 주입

### 6.3 언어별 주입

#### Java
**파일:** `injector_java.go`

```go
injectJavaEnvVars(container, cr, logger)
  ├─ JAVA_TOOL_OPTIONS 설정 (-javaagent:/whatap-agent/whatap.agent.java.jar)
  ├─ 환경 변수 추가:
  │  ├─ license: Whatap 라이선스
  │  ├─ whatap.server.host: Whatap 서버
  │  ├─ whatap.server.port: Whatap 포트
  │  ├─ whatap.micro.enabled: true
  │  ├─ NODE_IP: status.hostIP (Downward API)
  │  ├─ NODE_NAME: spec.nodeName
  │  └─ POD_NAME: metadata.name
  └─ 기존 컨테이너 환경 변수에 추가 (덮어쓰기 안 함)
```

#### Python
**파일:** `injector_python.go`

```go
injectPythonEnvVars(container, target, cr, version, logger)
  ├─ 타겟 환경 변수에서 앱 설정 읽기
  ├─ 환경 변수 설정:
  │  ├─ license, whatap_server_host, whatap_server_port
  │  ├─ app_name, app_process_name
  │  ├─ WHATAP_HOME: /whatap-agent
  │  ├─ PYTHONPATH: /whatap-agent/whatap/bootstrap + 기존
  │  └─ Kubernetes 메타데이터
  └─ whatap.conf 생성을 위한 새 구조
```

#### Node.js
**파일:** `injector_nodejs.go`

```go
injectNodejsEnvVars(container, cr)
  ├─ 환경 변수 설정:
  │  ├─ WHATAP_LICENSE, WHATAP_SERVER_HOST, WHATAP_SERVER_PORT
  │  ├─ whatap.micro.enabled: true
  │  └─ Kubernetes 메타데이터
  └─ Java/Python보다 단순한 주입
```

### 6.4 웹훅 상수

**파일:** `constants.go`

```go
// 환경 변수
EnvWhatapLicense, EnvWhatapHost, EnvWhatapPort
EnvNodeIP, EnvNodeName, EnvPodName
EnvJavaLicense, EnvJavaWhatapHost, EnvJavaWhatapPort, EnvJavaToolOptions
EnvPythonLicense, EnvPythonWhatapHost, EnvPythonWhatapPort, EnvAppName
EnvNodeLicense, EnvNodeWhatapHost, EnvNodeWhatapPort

// 상수
InitContainerName = "whatap-agent-init"
VolumeNameWhatapAgent = "whatap-agent-volume"
MountPathWhatapAgent = "/whatap-agent"
```

---

## 7. 주요 설정 파일

### 7.1 go.mod

**Go 버전:** 1.24.0+

**핵심 의존성:**
| 패키지 | 버전 | 설명 |
|--------|------|------|
| `sigs.k8s.io/controller-runtime` | v0.20.4 | Operator 프레임워크 |
| `k8s.io/api` | v0.33.0 | Kubernetes API |
| `k8s.io/apimachinery` | v0.33.0 | Kubernetes 유틸리티 |
| `k8s.io/client-go` | v0.33.0 | Kubernetes 클라이언트 |
| `gopkg.in/yaml.v2` | v2.4.0 | YAML 파싱 |
| `github.com/go-logr/logr` | v1.4.2 | 로깅 |
| `github.com/onsi/ginkgo/v2` | v2.22.0 | 테스팅 프레임워크 |

### 7.2 Dockerfile

**멀티 스테이지 빌드:**

1. **Builder Stage:**
   - Base: golang:1.24.3
   - Go 바이너리 컴파일
   - 출력: `/workspace/manager`

2. **Runtime Stage:**
   - Base: alpine:latest
   - 최소 크기
   - User: 65532:65532 (non-root)
   - Entrypoint: `/manager`

### 7.3 Makefile 주요 타겟

| 타겟 | 설명 |
|------|------|
| `make build` | 바이너리 컴파일 |
| `make docker-build` | Docker 이미지 빌드 |
| `make docker-push` | 레지스트리에 푸시 |
| `make deploy` | 클러스터에 배포 |
| `make undeploy` | 클러스터에서 제거 |
| `make test` | 테스트 실행 |
| `make bundle` | Operator 번들 생성 |
| `make bundle-build` | 번들 이미지 빌드 |

**설정:**
- VERSION: 0.4.2
- IMAGE_TAG_BASE: whatap.com/whatap-operator
- OPERATOR_SDK_VERSION: v1.39.1
- ENVTEST_K8S_VERSION: 1.31.0

---

## 8. 웹훅 설정

**웹훅 서버:**
- 포트: 9443
- 인증서 디렉토리: `/etc/webhook/certs`
- 인증서 파일: `tls.crt`, `tls.key`
- CA 인증서: `ca.crt`, `ca.key`

**서비스:**
- 이름: `whatap-admission-controller`
- 네임스페이스: 기본 네임스페이스 (whatap-monitoring)
- 포트: 443 → 9443

**MutatingWebhookConfiguration:**
- 이름: `whatap-webhook`
- 실패 정책: Ignore (비차단)

---

## 9. Operator 동작 방식

### 9.1 모니터링 대상

1. **WhatapAgent CR** - 에이전트 설정
2. **WhatapPodMonitor** - 파드 메트릭 스크레이핑 설정
3. **WhatapServiceMonitor** - 서비스 메트릭 스크레이핑 설정
4. **소유 리소스:**
   - Deployments (Master Agent, OpenAgent)
   - DaemonSets (Node Agent)
   - Services (웹훅, DCGM exporter)
   - ConfigMaps (에이전트 설정, DCGM 메트릭)
   - Secrets (웹훅 TLS 인증서)
   - ServiceAccounts
   - ClusterRoles, ClusterRoleBindings
   - MutatingWebhookConfiguration

### 9.2 관리 대상

1. **Kubernetes 인프라 모니터링:**
   - Master Agent (Deployment) - kube-apiserver, 컨트롤 플레인 모니터링
   - Node Agent (DaemonSet) - 노드, 컨테이너 런타임 모니터링
   - 선택적: etcd, scheduler, apiserver 모니터링

2. **Application Performance Monitoring (APM):**
   - 사용자 파드에 APM 에이전트 주입
   - 지원: Java, Python, Node.js, PHP, DotNet, Go
   - mutating webhook + init 컨테이너를 통해

3. **GPU 모니터링:**
   - NVIDIA DCGM exporter 배포
   - 메트릭 수집 및 노출
   - 메모리 사용량 모니터링

4. **OpenAgent (메트릭 스크레이핑):**
   - 독립형 prometheus 호환 메트릭 수집기
   - ServiceMonitor/PodMonitor 대상 스크레이핑
   - 메트릭 relabeling 지원

### 9.3 재조정 흐름

```
WhatapAgent CR 업데이트됨
  ↓
컨트롤러가 변경 감지
  ↓
Reconcile() 호출
  ├─ finalizer 추가 (생성 시)
  ├─ 삭제 타임스탬프 확인
  ├─ 삭제 중인 경우:
  │  ├─ 에이전트 정리
  │  ├─ finalizer 제거
  │  └─ 종료
  ├─ 상태를 "Progressing"으로 설정
  ├─ 웹훅 서비스 보장
  ├─ 웹훅 TLS 시크릿 보장
  ├─ MutatingWebhookConfiguration 보장
  ├─ Master Agent 활성화된 경우:
  │  └─ Master Agent Deployment 생성/업데이트
  ├─ Node Agent 활성화된 경우:
  │  └─ Node Agent DaemonSet 생성/업데이트
  ├─ GPU 모니터링 활성화된 경우:
  │  ├─ GPU ConfigMap 생성
  │  └─ DCGM exporter 컨테이너 추가
  ├─ OpenAgent 활성화된 경우:
  │  └─ OpenAgent Deployment 생성/업데이트
  └─ 상태를 "Available"로 설정
```

### 9.4 핵심 기능

| 기능 | 설명 |
|------|------|
| 선언적 설정 | CR로 모든 것 관리 |
| 자동 주입 | 웹훅을 통한 APM 에이전트 |
| 다중 언어 지원 | Java, Python, Node.js 등 |
| 보안 컨텍스트 커스터마이징 | Root/non-root 옵션 |
| 리소스 관리 | CPU/메모리 제한 설정 가능 |
| 노드 어피니티/톨러레이션 | 유연한 스케줄링 |
| 커스텀 이미지 | 프라이빗 레지스트리 지원 |
| 환경 변수 오버라이드 | 컴포넌트별 커스텀 환경 변수 |
| GPU 모니터링 | NVIDIA GPU 메트릭 |
| Finalizer 기반 정리 | 안전한 리소스 삭제 |

---

## 10. 배포 아키텍처

**Operator가 생성하는 컴포넌트:**

```
클러스터
├─ whatap-operator-system 네임스페이스
│  └─ whatap-operator 컨트롤러 파드
│
├─ whatap-monitoring 네임스페이스 (기본값)
│  ├─ whatap-admission-controller Service
│  ├─ whatap-webhook-certificate Secret
│  ├─ whatap-master-agent Deployment
│  │  └─ Master Agent 파드
│  ├─ whatap-node-agent DaemonSet
│  │  └─ Node Agent 파드 (노드당 하나)
│  ├─ whatap-open-agent Deployment (활성화된 경우)
│  │  └─ OpenAgent 파드
│  ├─ whatap-open-agent-config ConfigMap
│  ├─ whatap-gpu-dcgm-csv ConfigMap (GPU 활성화된 경우)
│  └─ whatap-dcgm-exporter Service (GPU 활성화된 경우)
│
├─ 사용자 네임스페이스
│  └─ 주입된 APM 에이전트가 있는 파드 (대상인 경우)
│     ├─ whatap-agent-init Init 컨테이너
│     ├─ App 컨테이너 (환경 변수 포함)
│     └─ whatap-agent-volume 볼륨
│
└─ 클러스터 범위 리소스
   ├─ WhatapAgent CR (클러스터 스코프)
   ├─ WhatapPodMonitor CRD
   ├─ WhatapServiceMonitor CRD
   └─ whatap-webhook MutatingWebhookConfiguration
```

---

## 11. 설정 예제

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  license: "YOUR_LICENSE_KEY"
  host: "whatap.server.com"
  port: "6600"
  features:
    apm:
      instrumentation:
        enabled: true
        targets:
          - name: java-services
            enabled: true
            language: java
            whatapApmVersions:
              java: "2.2.58"
            namespaceSelector:
              matchLabels:
                apm: enabled
            podSelector:
              matchLabels:
                app: java-app
            config:
              mode: default

    k8sAgent:
      namespace: whatap-monitoring
      masterAgent:
        enabled: true
        resources:
          requests:
            cpu: 250m
            memory: 512Mi
          limits:
            cpu: 500m
            memory: 1Gi

      nodeAgent:
        enabled: true
        runtime: containerd
        hostNetwork: true
        resources:
          requests:
            cpu: 100m
            memory: 256Mi
          limits:
            cpu: 300m
            memory: 512Mi

      gpuMonitoring:
        enabled: false

    openAgent:
      enabled: false
```

---

## 12. 주요 파일 요약

| 파일 경로 | 크기 | 목적 |
|-----------|------|------|
| `/cmd/main.go` | 350줄 | Operator 진입점, 인증서 생성 |
| `/api/v2alpha1/whatapagent_types.go` | 593줄 | WhatapAgent CRD 정의 |
| `/internal/controller/whatapagent_controller.go` | 712줄 | 메인 Reconciler 로직 |
| `/internal/controller/install_agents.go` | 2,343줄 | 에이전트 배포 생성 |
| `/internal/webhook/v2alpha1/whatapagent_webhook.go` | 200+줄 | 웹훅 등록 |
| `/internal/webhook/v2alpha1/process_deployments.go` | 100+줄 | Init 컨테이너 생성 |
| `/internal/webhook/v2alpha1/injector_java.go` | 80+줄 | Java APM 주입 |
| `/internal/webhook/v2alpha1/injector_python.go` | 80+줄 | Python APM 주입 |
| `/internal/webhook/v2alpha1/injector_nodejs.go` | 32줄 | Node.js APM 주입 |
| `/internal/webhook/v2alpha1/utils.go` | 120+줄 | 공통 유틸리티 |
| `/internal/controller/gpu_mem_check.go` | 6KB | GPU 메모리 모니터링 |
| `/config/rbac/role.yaml` | - | ClusterRole 권한 |
| `/config/manager/manager.yaml` | 96줄 | Operator 배포 |
| `/Dockerfile` | 38줄 | 멀티 스테이지 Docker 빌드 |
| `/Makefile` | 450+줄 | 빌드 자동화 |

---

## 13. 실행 흐름 요약

```
1. 배포 (kubectl apply)
   └─ whatap-operator-system 네임스페이스에 operator 파드 배포

2. 시작 (main.go)
   ├─ 플래그 & 환경 변수 파싱
   ├─ 웹훅 TLS 인증서 생성
   ├─ 웹훅 서버 생성 (포트 9443)
   ├─ 매니저 초기화
   ├─ 스킴 등록
   ├─ 컨트롤러 & 웹훅 설정
   └─ 재조정 루프 시작

3. 사용자가 WhatapAgent CR 생성
   └─ kubectl apply -f whatap-agent.yaml

4. 컨트롤러가 CR 감지
   └─ Reconcile() 트리거

5. Reconcile 실행
   ├─ finalizer 추가
   ├─ 웹훅 인프라 생성
   ├─ 에이전트 배포 (Master, Node, OpenAgent)
   ├─ 상태 업데이트
   └─ 변경 감시

6. 사용자 네임스페이스에서 파드 생성
   ├─ Kubernetes가 AdmissionReview를 웹훅에 전송
   ├─ WhatapAgentCustomDefaulter.Default()
   ├─ 파드가 대상과 매칭되는지 확인
   ├─ init 컨테이너 + 환경 변수 주입
   └─ 파드 생성 계속

7. 파드 실행
   ├─ Init 컨테이너 실행
   │  ├─ APM 에이전트 라이브러리 다운로드
   │  └─ 공유 볼륨에 마운트
   ├─ App 컨테이너 시작
   │  ├─ 환경 변수를 통해 APM 에이전트 로드
   │  └─ 애플리케이션 모니터링
   └─ 메트릭을 Whatap 서버로 전송

8. 메트릭 수집 (OpenAgent 활성화된 경우)
   ├─ ServiceMonitor/PodMonitor 대상 스크레이핑
   ├─ relabeling 적용
   └─ Whatap으로 전송

9. 정리 (CR 삭제 시)
   ├─ 에이전트 삭제 (Master, Node, OpenAgent)
   ├─ 웹훅 인프라 삭제
   ├─ finalizer 제거
   └─ CR 삭제됨
```

---

## 결론

**whatap-operator**는 Whatap APM을 위한 포괄적인 모니터링 및 계측 기능을 제공하는 정교한 Kubernetes Operator입니다.

**주요 특징:**
- **Controller-runtime 프레임워크** - 강력한 재조정
- **Mutating Webhooks** - 투명한 APM 주입
- **Custom Resources** - 선언적 설정
- **다중 언어 지원** - Java, Python, Node.js 등
- **인프라 모니터링** - Master/Node 에이전트, GPU
- **메트릭 수집** - Prometheus 호환 OpenAgent
- **보안 중심 설계** - non-root 컨테이너, RBAC, 네트워크 정책

이 Operator는 복잡한 Kubernetes 리소스 관리를 성공적으로 추상화하여, 사용자가 단일 CRD 설정으로 포괄적인 모니터링을 배포할 수 있게 합니다.

---

*문서 생성일: 2026-01-22*
*분석 대상 버전: 0.4.2*
