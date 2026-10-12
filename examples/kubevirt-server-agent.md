# 신규 KubeVirt VM의 서버 에이전트 자동 설치

## 범위

이 기능은 명시적으로 선택한 **새 `kubevirt.io/v1 VirtualMachine`의 CREATE** 요청에 cloud-init 초기 설치 설정을 넣는다. 기존 VM UPDATE, VMI, virt-launcher Pod는 변경하지 않으며 SSH/QEMU Guest Agent 접근을 사용하지 않는다. 실제 VM 이미지에는 cloud-init이 설치·활성화돼 있고, 아직 초기화되지 않은 새 인스턴스 상태여야 한다. 새 Kubernetes VM 객체가 재사용 OS 디스크의 cloud-init 상태까지 초기화한다는 뜻은 아니다.

현재 작업은 Operator 기능 개발이며 설치 화면 UI, 에이전트 업데이트/제거, 기존 실행 중 VM 설치, Windows 지원을 포함하지 않는다. 기능은 기본 비활성이다.

## 설정

1. 서버 모니터링 프로젝트의 키를 `targets[].license`에 직접 입력하거나, 같은 VM Namespace의 Secret을 `licenseSecretRef`로 참조한다. 둘 중 하나만 지정하며 별도의 `whatap-server-license` Secret 생성은 필수가 아니다. Kubernetes/APM 프로젝트의 키를 임의로 재사용하지 않는다. 직접 입력한 값은 CR 조회 권한이 있는 사람에게 보이므로 실제 키가 들어간 YAML은 Git·티켓·로그에 남기지 않는다.
2. 기존 `WhatapAgent/whatap` CR의 다른 feature 설정은 유지하고 `spec.features.kubeVirt` 부분만 추가한다. `examples/kubevirt-server-agent.yaml`은 구조 예제이며 실제 라이선스·수집 주소·PVC 준비가 필요하다.
3. `targets[].namespace`는 정확한 VM Namespace이고, `vmSelector`는 VM의 **최상위 metadata.labels**를 선택한다. Namespace 또는 Pod template label을 대신 보지 않는다.
4. 신규 VM에 `monitoring.whatap.com/server-agent: "true"` 라벨을 명시한다. 이 opt-in과 target selector가 모두 일치해야 설치 설정을 주입한다. 여러 target이 동시에 일치하면 임의로 첫 번째를 고르지 않고 거부한다.
5. 새 Operator 이미지, 갱신한 WhatapAgent CRD, Operator ServiceAccount의 Secret `get/create` 권한이 필요하다. 별도 Helm 차트에서 CRD/RBAC를 관리한다면 이 변경도 함께 반영해야 한다. 코드 작성만으로 기존 설치 버전이 기능을 지원하는 것은 아니다.
6. VM 생성 전 `whatap-webhook`에 `kubevirt-server.monitoring.whatap.com` 등록과 Operator webhook readiness를 확인한다. 준비 전에 VM을 먼저 만들면 첫 부팅을 놓칠 수 있다.

`host`는 수집 서버 IP/DNS 이름이며 여러 주소는 `/`로 구분한다. `port` 기본값은 6600이다. `license`와 `licenseSecretRef`를 동시에 지정하거나 둘 다 생략하면 거부한다. 참조 방식을 선택한 경우에는 VM과 같은 Namespace의 Secret name/key를 지정하며 optional 참조는 지원하지 않는다. 입력 방식과 관계없이 VM 부팅용 cloud-init Secret은 Operator가 자동 생성한다.

`spec.features.kubeVirt.targets[].agentVersion`으로 설치할 서버 에이전트 버전을 지정한다. 생략하면 **2.9.25**를 사용하며 `latest`를 따라가지 않는다. 값은 공식 저장소에 게시된 `X.Y.Z` 형식의 버전이어야 한다(최대 32자). 예제 YAML의 `agentVersion: "2.9.25"` 한 줄을 바꾸면 된다. 존재하지 않는 버전은 다운로드 단계에서 실패하며 다른 버전으로 자동 대체하지 않는다. 변경한 값은 **이후 생성하는 VM에만** 적용되고 기존 VM은 업그레이드하거나 재시작하지 않는다.

## 게스트 설치 환경

설치 경로는 Debian/Ubuntu와 RHEL 계열(`rhel`, `centos`, `rocky`, `almalinux`, `ol`, `amzn`) 모두 공식 tar의 `install.sh`로 통일한다. Fedora와 목록 밖의 배포판은 제외한다. 게스트의 `uname -m` 값으로 x86_64는 amd64, aarch64/arm64는 arm64 tar를 선택한다. 배포판 이름을 허용한다고 모든 OS 버전·아키텍처 조합의 실제 동작을 검증한 것은 아니다.

첫 부팅에서 `https://repo.whatap.io/infra/whatap-infra-{agentVersion}-linux-{arch}.tar.gz`를 받고, 설치 여부를 rpm/dpkg로 조회하거나 설치를 건너뛰지 않고 공식 installer를 실행한다. tar는 rpm/dpkg에 등록하지 않는다. `install.sh`는 서비스 등록만 하므로 bootstrap이 `systemctl enable`, `systemctl start`, `systemctl is-active`를 별도로 호출한다. 다운로드·설치·서비스 시작 중 하나라도 실패하면 성공으로 기록하지 않는다. 공식 HTTPS 다운로드와 archive/버전 검증을 사용하며, 패키지 저장소의 GPG 서명 검증과 같다고 주장하지 않는다.

이 기능의 정상 대상은 에이전트가 없는 새 cloud-init 이미지다. **새 VM 객체**라도 에이전트가 들어 있는 이미지나 이미 사용한 PVC에서 부팅할 수 있다는 점만 주의하면 된다. 기존 VM에 찾아가 덮어쓰는 기능은 없으며, 이미지 안의 외부 에이전트 설정 충돌은 자동 인계·업그레이드 범위가 아니다.

설치 스크립트는 `/usr/local/sbin/whatap-kubevirt-install.sh`, 입력 설정은 `/etc/whatap-kubevirt/whatap.conf`에 root-only로 기록된다. `/var/lib/whatap-kubevirt/success`는 해당 실행의 tar 설치·서비스 기동 성공만 의미하며 실제 지표 수신 성공은 아니다. cloud-init은 처음 설치를 실행하고, 관리자가 스크립트를 수동으로 다시 실행하면 tar를 다시 받아 installer를 호출한다. 자동 업데이트·무한 재시도 기능은 아니다.

## cloud-init 보존 및 Secret 처리

- NoCloud 및 ConfigDrive를 지원한다. 기존 `userData`, `userDataBase64`, `secretRef`의 `userdata`/`userData` 키를 읽는다. 기존 source Secret은 수정하지 않는다.
- 지원 형식은 빈 user data 또는 YAML `#cloud-config`다. 기존 일반 설정과 `write_files`/`runcmd` 목록에 설치 작업을 합친다. shell/MIME 형식, 모호한 YAML 또는 충돌하는 예약 경로는 명확히 거부한다.
- KubeVirt inline user data 크기 제한을 피하고 라이선스가 VM 명세에 노출되지 않도록, 병합 결과는 VM Namespace의 별도 immutable Secret에 저장한다. VM에는 `secretRef.name`만 들어간다. 기존 `networkData`와 root disk는 보존한다.
- webhook은 실 CREATE에서 Secret `get/create`만 수행하며 기존 Secret을 덮어쓰지 않는다. dry-run은 예상 VM patch만 반환하고 Secret을 만들지 않는다(`sideEffects: NoneOnDryRun`).
- 생성 Secret 이름은 정책 UID·VM Namespace/이름·병합 결과로 결정해 동일 입력 재시도 시 재사용한다. 다른 주체의 동일 이름 Secret은 채택하지 않는다.
- 생성 Secret은 WhatapAgent CR 소유다. VM 삭제 또는 이후 admission 단계에서 VM 생성 거절만으로 즉시 삭제되지 않는다. source Secret과 생성 Secret은 비밀정보로 취급하고, 더 이상 어떤 VM도 참조하지 않는 결과만 관리자가 정리한다. 정책 CR 삭제에 따른 GC 전에는 해당 Secret을 참조하는 VM의 재시작/재생성 영향을 검토해야 한다.
- 정책/라이선스 변경은 기존 VM에 자동 재설치·자동 갱신하지 않는다. 이 기능은 신규 VM 초기 설치 계약이다.

## 상태와 실패 처리

VM의 `monitoring.whatap.com/server-agent-bootstrap` annotation은 준비된 cloud-init Secret 이름이다. **설치 완료 또는 수집 성공이 아니다.**

확인 단계는 별도로 구분한다.

1. VM에 올바른 cloud-init Secret 참조가 주입됐는지 확인.
2. 게스트에서 `cloud-init status --wait`로 초기화 종료 확인.
3. `systemctl status whatap-infra`로 서버 에이전트 서비스 확인.
4. 와탭 서버 모니터링에서 대상 등록과 실제 지표 수신 확인.

cloud-init 활성화, 지원 OS·아키텍처·systemd·Bash, curl·tar 및 신뢰할 수 있는 CA 인증서, 공식 tar 저장소 HTTPS 접근과 수집 서버 TCP outbound는 게스트의 사전 조건이다. 설치 실패는 VM 부팅 성공과 별개이며, Operator가 VM을 재부팅하거나 임의로 cloud-init 캐시를 초기화하지 않는다.

전송 실패를 무시하고 VM이 무주입 상태로 첫 부팅하는 것을 막기 위해 새 webhook은 opt-in 라벨 VM에만 `failurePolicy: Fail`로 등록한다. 일반 VM과 기존 APM webhook의 정책은 변경하지 않는다. `kubeVirt.enabled=false`는 이 webhook 등록을 제거하지만 이미 설치된 게스트 에이전트는 제거하지 않는다.

## 근거

- https://kubevirt.io/user-guide/user_workloads/startup_scripts/
- https://docs.cloud-init.io/en/latest/explanation/boot.html
- https://docs.whatap.io/en/server/server-os/server-linux
- KubeVirt v1.6.4 API: `CloudInitNoCloudSource.UserDataSecretRef`의 JSON 이름은 `secretRef`.
- KubeVirt v1.6.4 validation: inline userData 최대 2048 bytes이며 더 큰 데이터는 Secret 참조 권장.
