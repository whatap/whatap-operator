# Python custom 설정과 자동 로딩

Python 자동 주입은 `config.mode: default`와 `custom`에서 동일한 연결정보와
자동 로딩 경로를 애플리케이션 컨테이너에 전달한다.

## 연결 설정

- 기존 `whatap_server_host` / `whatap_server_port` 환경변수를 유지한다.
- 네이티브 수집 모듈이 읽는 `WHATAP_SERVER_HOST` / `WHATAP_SERVER_PORT`와
  `whatap.server.host` / `whatap.server.port`에도 같은 연결정보를 주입한다.
- CR의 지원되는 target 환경변수 override 또는 Operator 기본 연결정보를 사용한다.
  기존 Pod 환경변수에 충돌하는 와탭 연결정보가 있으면 선택된 값으로 일치시킨다.
- `valueFrom`으로 지정한 ConfigMap/Secret 참조는 값으로 변환하거나 내용을 읽지 않고
  init 컨테이너와 애플리케이션 컨테이너까지 보존한다.
- custom `whatap.conf`는 수정하지 않는다. 수집 모듈의 기본 파일 우선순위를 유지하므로
  파일에 명시한 연결 설정은 계속 우선한다. `use_env_first`도 강제로 설정하지 않는다.
  키만 있고 값이 비어 있거나 잘못된 값이 있는 파일은 자동 보정하지 않는다.

## Python 자동 로딩

`PYTHONPATH`에 `/whatap-agent`와 `/whatap-agent/whatap/bootstrap`을 추가하고 기존
애플리케이션 경로를 보존한다. 반복 주입 시 이 두 경로를 중복 추가하지 않는다.
기존 경로의 빈 구간은 Python의 현재 디렉터리 탐색 의미를 유지하도록 보존한다.

Pod의 `PYTHONPATH`가 `valueFrom`이면 충돌하지 않는 내부 환경변수에 원래 참조를
보존한 뒤 Kubernetes의 `$(NAME)` 확장으로 합친다. 참조 변수는 `PYTHONPATH`보다
먼저 선언하며, 기존 `PYTHONPATH`를 참조하는 후속 변수들의 순서도 유지한다.
`optional: true`인 ConfigMap/Secret 참조에는 같은 내부 변수명으로 현재
`$(PYTHONPATH)`를 먼저 저장한다. 참조가 존재하면 해당 값으로 덮어쓰고, 리소스나
키가 없으면 kubelet이 참조 선언을 건너뛰어 앞선 선언 또는 `envFrom`의 경로가
유지된다. `optional`이 없거나 `false`인 참조의 필수 조회 동작은 바꾸지 않는다.
앞선 `PYTHONPATH`도 없으면 미해결 `$(PYTHONPATH)` 문자열은 그대로 남지만,
와탭 패키지·bootstrap 경로는 앞에 추가된다. 중복 선언은 마지막 항목만 보강하며,
앞선 선언과 중간 참조는 원래 순서를 유지하고 반복 주입 시 내부 변수가 늘지 않는다.

애플리케이션 command/args, 실행 UID, 보안 컨텍스트는 변경하지 않는다.

## 적용 및 확인

Operator 업그레이드만으로 기존 Pod의 환경변수는 바뀌지 않는다. 승인된 rollout으로
새 Pod를 생성한 뒤 init 완료, Python 자동 로딩, 기동 파일 생성, 실제 수집을 각각 확인한다.
주입 성공이나 init 완료만으로 실제 APM 수집 성공을 판단하지 않는다.
이 변경은 설정·주입 경로 보완이며 고객 환경의 복구 완료를 의미하지 않는다.

## 회귀 테스트

- default/custom의 연결정보 별칭 일치 및 중복 제거
- 연결정보의 ConfigMap/Secret 참조 보존
- 기존 PYTHONPATH 없음/빈 값/기존 경로/빈 경로 구간/반복 주입
- PYTHONPATH의 ConfigMap/Secret 참조, 내부 변수명 충돌, 선언 순서
- 입력 Pod/환경변수 비변경

실행: `go test -race ./api/... ./internal/...`
