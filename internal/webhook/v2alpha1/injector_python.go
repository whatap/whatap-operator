package v2alpha1

import (
	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
)

func injectPythonEnvVars(container corev1.Container, target monitoringv2alpha1.TargetSpec, cr monitoringv2alpha1.WhatapAgent, version string, logger logr.Logger) []corev1.EnvVar {
	logger.Info("Configuring Python APM agent injection with whatap.conf", "version", version)

	// Read from target.Envs (align with Java approach)
	appName, appProcessName, okind := getPythonAppConfig(target.Envs)
	// Preserve previous default behavior: if appName is empty, use container name
	if appName == "" {
		appName = container.Name
	}

	// Python 전용 환경변수 추가 (CR 기반)
	licenseEnv := getWhatapLicenseEnvVar(cr)
	licenseEnv.Name = EnvPythonLicense // Python agent expects "license" env var name

	hostEnv := getWhatapHostEnvVar(cr)
	hostEnv.Name = EnvPythonWhatapHost // Python agent expects "whatap_server_host" env var name

	portEnv := getWhatapPortEnvVar(cr)
	portEnv.Name = EnvPythonWhatapPort

	// Python APM 환경변수 구성
	envVars := []corev1.EnvVar{
		// Whatap 서버 연결 정보
		licenseEnv,
		hostEnv,
		portEnv,

		// Python 애플리케이션 정보
		{Name: EnvAppName, Value: appName},
		{Name: EnvAppProcessName, Value: appProcessName},

		// Python 에이전트 경로 설정 (새로운 구조)
		{Name: EnvWhatapHome, Value: ValWhatapHome},
		// Whatap 설정
		{Name: EnvWhatapMicroEnabled, Value: ValTrue},

		// Kubernetes 메타데이터
		{Name: EnvNodeIP, ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}}},
		{Name: EnvNodeName, ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}}},
		{Name: EnvPodName, ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
	}

	// Add OKIND if provided
	if okind != "" {
		envVars = append(envVars, corev1.EnvVar{Name: EnvOkind, Value: okind})
	}

	// PYTHONPATH 안전하게 주입 (새로운 구조)
	envVars = injectPythonPath(envVars, ValPythonBootstrap, logger)

	// WHATAP_PYTHON_AGENT_PATH 기본값 설정: 사용자가 지정하지 않은 경우에만 추가
	// 우선순위: 컨테이너에 이미 존재하면 그대로 유지
	hasPythonAgentPath := false
	for _, e := range container.Env {
		if e.Name == EnvPythonAgentPath {
			hasPythonAgentPath = true
			break
		}
	}
	if !hasPythonAgentPath {
		// envVars에 동일 키가 있다면 추가하지 않음(이 경우도 드뭅니다만 안전장치)
		for _, e := range envVars {
			if e.Name == EnvPythonAgentPath {
				hasPythonAgentPath = true
				break
			}
		}
		if !hasPythonAgentPath {
			envVars = append(envVars, corev1.EnvVar{Name: EnvPythonAgentPath, Value: ValPythonAgentPath})
		}
	}

	return append(container.Env, envVars...)
}

// PYTHONPATH 안전하게 주입 (OpenTelemetry 방식)
func injectPythonPath(envVars []corev1.EnvVar, bootstrapPath string, logger logr.Logger) []corev1.EnvVar {
	found := false
	for i, env := range envVars {
		if env.Name == EnvPythonPath {
			if env.ValueFrom != nil {
				logger.Info("PYTHONPATH is set via ConfigMap/Secret. Skipping injection.")
				found = true
				break
			} else {
				// 이미 값이 있는 경우: 앞쪽에 추가 (우선순위 높임) 또는 뒤쪽?
				// 보통 PYTHONPATH는 앞쪽이 우선. bootstrap을 앞에 두어 에이전트 로딩 보장
				// 구분자는 ':'
				logger.Info("Appending to existing PYTHONPATH", "original", env.Value)
				envVars[i].Value = bootstrapPath + ":" + env.Value
				found = true
				break
			}
		}
	}
	if !found {
		// 없으면 새로 추가
		envVars = append(envVars, corev1.EnvVar{Name: EnvPythonPath, Value: bootstrapPath})
	}
	return envVars
}

func getPythonAppConfig(envs []corev1.EnvVar) (string, string, string) {
	appName := "" // Default empty
	appProcessName := ""
	okind := ""

	for _, e := range envs {
		if e.Name == EnvAppName {
			appName = e.Value
		} else if e.Name == EnvAppProcessName {
			appProcessName = e.Value
		} else if e.Name == EnvOkind {
			okind = e.Value
		}
	}
	return appName, appProcessName, okind
}

func wrapPythonCommand(container *corev1.Container, logger logr.Logger) {
	// If the container has a command, prepend the whatap-start-agent
	// If not, we can't do much unless we know the entrypoint.
	// But usually for Python containers, Command or Args are defined.

	// Case 1: Command is defined
	if len(container.Command) > 0 {
		originalCommand := container.Command
		originalArgs := container.Args

		// Construct new command
		// whatap-start-agent [original command] [original args]
		// But wait, whatap-start-agent is a script.
		// /whatap-agent/bin/whatap-start-agent
		// It executes the passed command with instrumentation.

		newArgs := make([]string, 0)
		newArgs = append(newArgs, originalCommand...)
		newArgs = append(newArgs, originalArgs...)

		logger.Info("Wrapping Python application command with whatap-start-agent", "originalCommand", originalCommand, "originalArgs", originalArgs)

		container.Command = []string{"/whatap-agent/bin/whatap-start-agent"}
		container.Args = newArgs
	} else {
		// Case 2: Only Args defined (Entrypoint is implicit or in image)
		// We can try to prepend to Args if we assume Entrypoint is python
		// But if Entrypoint is a script, it might work too.
		// safest is if we can set Command.
		// If Command is empty, k8s uses Image Entrypoint.
		// We can't wrap invisible Entrypoint easily without changing Command.
		// Let's assume the user provided Command or we can just prepend to Args if Command is empty?
		// No, if Command is empty, we set Command to whatap-start-agent and move original Args to new Args.
		// BUT we don't know the original Entrypoint!
		// If we overwrite Command, original Entrypoint is ignored.
		// So we can only support cases where we know what to run.

		// For now, let's only support cases where we explicitly wrap if we can guess.
		// Or, if this function is called, we assume the user accepts wrapper.
		// But without knowing original Entrypoint, we risk breaking it.

		// However, in many Python Docker images, Entrypoint is `python` or undefined.
		// If undefined, we can't guess.

		// Current strategy: Only modify if we can see the command.
		// This might be limited.
		// (Legacy behavior preserved)
	}
	// Actually, looking at previous logic (lines 324-343 of original), it handled:
	// if len(container.Command) > 0 { ... }
	// It did NOT handle empty Command.
	// We preserve this logic.
}
