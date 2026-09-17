# GPU 모니터링 설치 전 점검 및 dcgm-exporter 호환 기준

> 문서 소유자: Kubernetes Agent Team<br>
> 최종 검증일: 2026-08-27<br>
> 갱신 트리거: Whatap Operator/Helm chart 기본 이미지 변경, NVIDIA DCGM Exporter 또는 드라이버 지원 정책 변경, GPU/MIG 관련 장애 발생

이 문서는 고객 환경에서 Whatap GPU 모니터링을 활성화하기 전에 사용할 표준 절차다. 표에 없는 조합은 지원 조합으로 추정하지 말고 **미검증**으로 처리한다.

## 1. 사전 입력표

아래 값을 티켓에 그대로 첨부한다. 값이 하나라도 확인되지 않으면 설치를 시작하지 않는다.

| 항목 | 확인 값 |
| --- | --- |
| Whatap Operator Helm chart / appVersion | `helm list -A`, `helm show chart whatap/whatap-operator` 결과 |
| GPU 모델 / 아키텍처 / 수량 | `nvidia-smi -L` 결과(예: A100/Ampere) |
| NVIDIA driver / 표시 CUDA 버전 | `nvidia-smi` 결과 |
| NVML | `nvidia-smi -q` 성공 여부와 오류 전문 |
| Kubernetes / 노드 OS / CPU 아키텍처 | `kubectl version`, `kubectl get node -o wide`, `uname -m` |
| Container runtime | `kubectl get node -o jsonpath=...containerRuntimeVersion` |
| NVIDIA Container Toolkit / GPU Operator | 설치 방법과 chart/version |
| 런타임에서 GPU 노출 | 아래 점검 Pod 결과 |
| MIG mode / profile / topology | `nvidia-smi -q -d MIG`, `nvidia-smi mig -lgip` 결과 |
| 기존 dcgm-exporter / nv-hostengine | Kubernetes 리소스 및 호스트 프로세스 점검 결과 |
| 폐쇄망 여부 / 사설 registry | registry 주소, 반입 담당자, 허용 CPU 아키텍처 |

비밀값, registry 인증 토큰, Whatap 라이선스 키는 티켓에 첨부하지 않는다.

## 2. 릴리스별 호환표

상태 정의:

- **지원**: Operator 기본 이미지와 NVIDIA가 짝지어 배포한 DCGM/Exporter 조합을 사용하며 이 문서의 사전·사후 점검을 통과했다.
- **미검증**: 최소 요구사항은 만족하지만 Whatap이 해당 GPU/driver/MIG 조합을 검증하지 않았다. 운영 설치 전에 동일 환경 검증이 필요하다.
- **비지원**: NVIDIA 요구사항 미충족, DCGM/Exporter pair 불일치, remote hostengine client/server 조건 위반, 또는 지원하지 않는 CPU 아키텍처다.

| Whatap chart / appVersion | 기본 이미지(immutable tag) | multi-arch digest | 포함 DCGM / Exporter | driver 기준 | MIG / hostengine 기준 | 상태 |
| --- | --- | --- | --- | --- | --- | --- |
| 1.9.9 / 3.0.16 | `public.ecr.aws/whatap/dcgm-exporter:4.6.0-4.8.3-distroless` | `sha256:96bdf764fc3119345bf1bc85ab3a431040d629e972e29a488aea319e40d8d571` | DCGM 4.6.0 / Exporter 4.8.3 | NVIDIA Datacenter Driver R450 이상은 기본 전제. GPU 아키텍처별 현재 지원 branch 및 CUDA/NVML 조합은 NVIDIA 지원표로 재확인 | embedded가 기본. 외부 hostengine 사용 시 exporter의 DCGM client(4.6.0) >= hostengine DCGM이어야 한다. MIG는 실제 profile에서 지표 노출 검증 필수 | 사전·사후 점검 통과 시 지원. 개별 GPU/driver 조합은 검증 기록이 없으면 미검증 |

태그의 첫 버전은 DCGM, 두 번째 버전은 Exporter다. 위 digest는 amd64와 arm64 manifest를 묶은 index digest다. 폐쇄망 반입 시 CPU 아키텍처별 manifest digest도 함께 기록한다.

이전 chart/appVersion은 배포된 Operator에서 실제 이미지를 조회하여 별도 행으로 추가하기 전까지 **미검증**이다. `latest` 또는 DCGM 버전만 있는 가변 태그는 사용하지 않는다.

## 3. 이미지 및 exporter 선택 규칙

1. 클러스터에 GPU Operator가 관리하는 exporter가 없으면 Whatap 내장 exporter를 사용한다.
2. 기존 exporter가 있으면 같은 노드에서 exporter/embedded hostengine을 중복 실행하지 않는다. 다음 중 하나만 선택한다.
   - Whatap exporter를 사용하고 기존 exporter를 해당 노드에서 비활성화한다.
   - 기존 exporter를 유지하고 `gpuMonitoring.enabled: false`로 둔 뒤 OpenAgent가 기존 `:9400/metrics`를 수집하도록 구성한다.
3. 기본 이미지가 환경 요구사항과 맞지 않을 때만 `customImageFullName`을 사용한다. 이미지에는 NVIDIA가 짝지어 배포한 DCGM/Exporter pair가 들어 있어야 하며 Whatap CSV의 모든 필드를 읽을 수 있어야 한다.
4. 외부 `nv-hostengine`에 연결할 경우 exporter 이미지의 DCGM client 버전이 hostengine 버전보다 낮으면 비지원이다.
5. 고객 exporter는 `/metrics`에서 최소한 `DCGM_FI_DEV_GPU_UTIL`, `DCGM_FI_DEV_FB_USED`, `DCGM_FI_DEV_FB_FREE`, `DCGM_FI_DEV_POWER_USAGE`, `DCGM_FI_DEV_GPU_TEMP`를 제공해야 한다. MIG 가중 사용률이 필요하면 `DCGM_FI_DEV_WEIGHTED_GPU_UTIL`도 필수다. 실제 필수 전체 목록은 Operator의 `internal/gpu/csv.go`를 기준으로 한다.

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
  namespace: whatap-monitoring
spec:
  features:
    k8sAgent:
      gpuMonitoring:
        enabled: true
        customImageFullName: registry.example.com/whatap/dcgm-exporter@sha256:<platform-manifest-digest>
```

## 4. 설치 전 표준 점검

읽기 전용 명령부터 실행한다.

```bash
nvidia-smi
nvidia-smi -L
nvidia-smi -q -d MIG
nvidia-smi mig -lgip

kubectl version
kubectl get nodes -o custom-columns='NAME:.metadata.name,OS:.status.nodeInfo.osImage,ARCH:.status.nodeInfo.architecture,RUNTIME:.status.nodeInfo.containerRuntimeVersion'
kubectl get runtimeclass
kubectl get ds,deploy,pod,svc -A -o wide | grep -Ei 'dcgm|gpu-operator|nvidia'
kubectl get pod -A -o jsonpath='{range .items[*]}{.metadata.namespace}{"\t"}{.metadata.name}{"\t"}{range .spec.containers[*]}{.image}{" "}{end}{"\n"}{end}' | grep -Ei 'dcgm|nvidia'
```

호스트 접근 권한이 있으면 기존 hostengine도 확인한다.

```bash
ps -ef | grep '[n]v-hostengine'
ss -lntp | grep ':5555'
```

GPU 런타임 노출은 고객이 승인한 테스트 namespace에서, 설치된 RuntimeClass와 정책에 맞는 임시 Pod로 검증한다. 운영 정책상 임시 Pod 생성이 금지되면 기존 GPU workload에서 `nvidia-smi` 결과를 받는다.

표준 결과 요약 형식:

```text
GPU/model/arch:
driver/CUDA/NVML:
Kubernetes/OS/CPU/runtime:
Toolkit/GPU Operator:
MIG mode/profiles:
existing exporter/hostengine:
selected mode: Whatap embedded | Whatap remote hostengine | customer exporter + OpenAgent
selected image tag@digest:
compatibility: supported | unverified | unsupported
reason/remaining validation:
```

## 5. 폐쇄망 반입

반입 목록은 Operator chart, Operator 이미지, kube agent 이미지, open agent 이미지, 선택한 dcgm-exporter 이미지와 선택 사항인 dcgm hostengine 이미지다. chart에서 실제 이미지 목록을 추출해 누락 여부를 확인한다.

```bash
helm template whatap whatap/whatap-operator -n whatap-monitoring -f values.yaml \
  | awk '$1 == "image:" {print $2}' | sort -u

docker buildx imagetools inspect public.ecr.aws/whatap/dcgm-exporter:4.6.0-4.8.3-distroless
```

외부망에서 이미지를 digest로 pull하고, 승인된 스캔을 거쳐 사설 registry에 push한다. 반입 전후 digest와 파일 checksum을 기록한다. 사설 registry의 platform manifest digest를 CR의 `customImageFullName`에 지정하고 `imagePullSecrets`를 별도로 구성한다. 서로 다른 registry의 digest 문자열이 같다는 가정은 하지 말고 실제 push 결과를 다시 조회한다.

## 6. 설치 후 검증

Pod Ready만으로 완료하지 않는다.

```bash
kubectl -n whatap-monitoring get pod -l name=whatap-node-agent -o wide
kubectl -n whatap-monitoring get ds whatap-node-agent -o jsonpath='{.spec.template.spec.containers[?(@.name=="dcgm-exporter")].image}{"\n"}'
kubectl -n whatap-monitoring logs -l name=whatap-node-agent -c dcgm-exporter --tail=200
kubectl -n whatap-monitoring logs -l app.kubernetes.io/name=whatap-open-agent --tail=200

POD=$(kubectl -n whatap-monitoring get pod -l name=whatap-node-agent -o jsonpath='{.items[0].metadata.name}')
kubectl -n whatap-monitoring exec "$POD" -c dcgm-exporter -- \
  wget -qO- http://127.0.0.1:9400/metrics | grep -E 'DCGM_FI_DEV_(GPU_UTIL|FB_USED|POWER_USAGE|GPU_TEMP|WEIGHTED_GPU_UTIL)' | head -50
```

다음을 모두 확인한다.

- `:9400/metrics`가 Prometheus text 형식으로 응답한다.
- GPU UUID/노드 라벨과 대표 지표가 실제 값을 가진다.
- MIG 환경이면 GI/CI 식별과 `DCGM_FI_DEV_WEIGHTED_GPU_UTIL`의 지원/미지원 사유가 명확하다.
- OpenAgent target/log에 scrape 오류가 없다.
- Whatap GPU 화면과 MXQL에서 동일 GPU UUID의 최신 데이터가 조회된다.

## 7. 중단 및 롤백

다음 중 하나면 추가 노드로 확대하지 않고 즉시 중단한다.

- 새로운 NVIDIA XID 오류가 반복된다.
- MIG entity subscription/field watch 오류가 반복되거나 필수 MIG 지표가 누락된다.
- `D` state 프로세스가 설치 전 기준선보다 지속적으로 증가한다.
- 같은 노드에 exporter 또는 hostengine이 중복 실행된다.
- exporter가 CrashLoop/OOM 상태이거나 GPU workload에 성능·안정성 영향이 관찰된다.
- image tag/digest가 승인 기록과 다르다.

롤백은 CR에서 GPU 모니터링만 비활성화한다. Operator나 전체 Kubernetes 에이전트를 제거하지 않는다.

```yaml
spec:
  features:
    k8sAgent:
      gpuMonitoring:
        enabled: false
```

변경 후 `dcgm-exporter` 컨테이너가 node-agent Pod에서 제거됐는지 확인하고, XID/D-state/GPU workload가 설치 전 기준으로 회복되는지 관찰한다. 고객 기존 exporter를 사용하던 구성이라면 OpenAgent target도 함께 원복한다.

## 8. 근거와 갱신 방법

- Operator 기본 이미지와 CR 필드: `internal/controller/install_agents.go`, `api/v2alpha1/whatapagent_types.go`
- Whatap 필수 CSV: `internal/gpu/csv.go`
- NVIDIA 설치 및 version pairing: <https://docs.nvidia.com/datacenter/dcgm/latest/installation/install-dcgm-exporter.html>
- NVIDIA 지원 환경/driver 기본 요건: <https://docs.nvidia.com/datacenter/dcgm/latest/user-guide/getting-started.html>
- NVIDIA exporter release: <https://github.com/NVIDIA/dcgm-exporter/releases>

기본 이미지가 바뀌는 PR은 이 표의 tag/digest/검증일을 같은 PR에서 갱신한다. chart/appVersion이 릴리스되면 해당 행을 추가하고, 실제 검증한 GPU 모델·driver·MIG profile만 **지원**으로 기록한다.
