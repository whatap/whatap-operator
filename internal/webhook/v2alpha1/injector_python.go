package v2alpha1

import (
	"fmt"
	"strings"

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

	// Python 전용 환경변수 추가 (CR 기반, target envs 오버라이드 지원)
	licenseEnv := getWhatapLicenseEnvVar(cr, target)
	licenseEnv.Name = EnvPythonLicense // Python agent expects "license" env var name

	hostEnv := getWhatapHostEnvVar(cr, target)
	hostEnv.Name = EnvPythonWhatapHost

	portEnv := getWhatapPortEnvVar(cr, target)
	portEnv.Name = EnvPythonWhatapPort

	// Keep the Python aliases for compatibility, and also supply the names read
	// by its native collector when a custom whatap.conf omits connection fields.
	// Do not change the collector's existing file-over-environment precedence.
	nativeHostEnv, nativePortEnv := hostEnv, portEnv
	nativeHostEnv.Name = EnvPythonNativeHost
	nativePortEnv.Name = EnvPythonNativePort
	// Exact dotted keys take precedence over uppercase aliases in the native
	// collector; keep all accepted names consistent with the resolved CR value.
	dottedHostEnv, dottedPortEnv := hostEnv, portEnv
	dottedHostEnv.Name = EnvJavaWhatapHost
	dottedPortEnv.Name = EnvJavaWhatapPort

	// Python APM 환경변수 구성
	envVars := []corev1.EnvVar{
		// Whatap 서버 연결 정보
		licenseEnv,
		hostEnv,
		portEnv,
		nativeHostEnv,
		nativePortEnv,
		dottedHostEnv,
		dottedPortEnv,

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

	// 와탭 소유 연결/설정 ENV(license, whatap_server_host/port, WHATAP_HOME, micro, downward-API)는
	// 기존 container.Env에 동일 키가 있어도 operator 값으로 강제 override (KAZAA-641).
	// Kubernetes는 중복 ENV의 마지막 선언을 사용한다. app_name/PYTHONPATH/agent path 등은 사용자 값을 보존한다.
	envVars = combineEnvVars(container.Env, envVars, func(name string) bool {
		_, ok := pythonForceEnvNames[name]
		return ok
	})
	// Augment the effective application environment, not just the new variables:
	// an existing-wins merge would otherwise discard the bootstrap path.
	return injectPythonPath(envVars, target.Envs, ValPythonBootstrap, logger)
}

// Preserve application paths and their runtime references while enabling the
// agent's sitecustomize.py. Never resolve ConfigMap/Secret contents in admission.
func injectPythonPath(envVars, reservedEnvVars []corev1.EnvVar, bootstrapPath string, logger logr.Logger) []corev1.EnvVar {
	lastPathIndex := -1
	for i, env := range envVars {
		if env.Name == EnvPythonPath {
			lastPathIndex = i
		}
	}
	result := make([]corev1.EnvVar, 0, len(envVars)+1)
	for i, env := range envVars {
		// Kubelet expands then overwrites in declaration order: only augment the
		// last value, leaving earlier values intact for consumers/self-references.
		if i != lastPathIndex {
			result = append(result, env)
			continue
		}
		if env.ValueFrom != nil {
			// Kubernetes expands $(NAME) in declaration order. Keep the referenced
			// value immediately before PYTHONPATH and before its existing consumers.
			source := env
			source.Name = pythonPathSourceName(envVars, reservedEnvVars)
			cm, secret := source.ValueFrom.ConfigMapKeyRef, source.ValueFrom.SecretKeyRef
			if (cm != nil && cm.Optional != nil && *cm.Optional) ||
				(secret != nil && secret.Optional != nil && *secret.Optional) {
				// Kubelet skips missing optional sources without assigning them.
				// Seed the alias from the effective path (including envFrom), so
				// a present source overwrites it and an absent one preserves it.
				result = append(result, corev1.EnvVar{Name: source.Name, Value: "$(" + EnvPythonPath + ")"})
			}
			result = append(result, source)
			env = corev1.EnvVar{Name: EnvPythonPath, Value: "$(" + source.Name + ")"}
			logger.Info("Preserving PYTHONPATH valueFrom with a runtime environment reference")
		}
		env.Value = prependPythonPaths(env.Value, bootstrapPath)
		result = append(result, env)
	}
	if lastPathIndex == -1 {
		result = append(result, corev1.EnvVar{Name: EnvPythonPath, Value: prependPythonPaths("", bootstrapPath)})
	}
	return result
}

func prependPythonPaths(value, bootstrapPath string) string {
	// The package root also matters: the bundled bootstrap's fallback does not
	// parse a colon-separated PYTHONPATH when importing the whatap package.
	paths := []string{ValWhatapHome, bootstrapPath}
	if value != "" {
		for _, path := range strings.Split(value, ":") {
			if path != ValWhatapHome && path != bootstrapPath {
				paths = append(paths, path)
			}
		}
	}
	return strings.Join(paths, ":")
}

func pythonPathSourceName(envVars, reservedEnvVars []corev1.EnvVar) string {
	used := make(map[string]bool, len(envVars)+len(reservedEnvVars))
	for _, envs := range [][]corev1.EnvVar{envVars, reservedEnvVars} {
		for _, env := range envs {
			used[env.Name] = true
		}
	}
	name := EnvPythonPathSource
	for suffix := 1; used[name]; suffix++ {
		name = fmt.Sprintf("%s_%d", EnvPythonPathSource, suffix)
	}
	return name
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
