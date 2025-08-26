package v2alpha1

import (
	"fmt"

	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	"github.com/whatap/whatap-operator/internal/config"
	corev1 "k8s.io/api/core/v1"
)

// Helper functions to get environment variables for Whatap credentials
// These functions read values directly from environment variables

// Helper functions for pointer types
func boolPtr(b bool) *bool    { return &b }
func int64Ptr(i int64) *int64 { return &i }

func getWhatapLicenseEnvVar(cr monitoringv2alpha1.WhatapAgent) corev1.EnvVar {
	license := config.GetWhatapLicense()
	return corev1.EnvVar{Name: "WHATAP_LICENSE", Value: license}
}

func createAgentInitContainers(target monitoringv2alpha1.TargetSpec, cr monitoringv2alpha1.WhatapAgent, lang, version string, logger logr.Logger) []corev1.Container {
	baseVolumeMount := corev1.VolumeMount{
		Name:      "whatap-agent-volume",
		MountPath: "/whatap-agent",
	}

	// SecurityContext for init containers (needs root access)
	securityContext := &corev1.SecurityContext{
		RunAsNonRoot: boolPtr(false),
		RunAsUser:    int64Ptr(0),
	}

	if lang == "python" {
		logger.Info("Using Python APM bootstrap init container with new structure", "version", version)

		// Get Python app configuration
		appName, appProcessName, OKIND := getPythonAppConfig(target.Envs)

		// Prepare environment variables for Python InitContainer
		envVars := []corev1.EnvVar{
			getWhatapLicenseEnvVar(cr),
			getWhatapHostEnvVar(cr),
			getWhatapPortEnvVar(cr),
			{Name: "APP_NAME", Value: appName},
			{Name: "APP_PROCESS_NAME", Value: appProcessName},
			{Name: "OKIND", Value: OKIND},
		}

		return []corev1.Container{
			{
				Name:            "whatap-agent-init",
				Image:           getAgentImage(target, lang, version),
				ImagePullPolicy: corev1.PullAlways,
				Env:             envVars,
				VolumeMounts:    []corev1.VolumeMount{baseVolumeMount},
				SecurityContext: securityContext,
			},
		}
	}

	if lang == "java" {
		logger.Info("Using Java APM init container with config generation", "version", version)

		// Java 설정을 위한 환경변수 준비
		envVars := []corev1.EnvVar{
			getWhatapLicenseEnvVar(cr),
			getWhatapHostEnvVar(cr),
			getWhatapPortEnvVar(cr),
		}

		return []corev1.Container{
			{
				Name:            "whatap-agent-init",
				Image:           getAgentImage(target, lang, version),
				ImagePullPolicy: corev1.PullAlways,
				Env:             envVars,
				VolumeMounts:    []corev1.VolumeMount{baseVolumeMount},
				SecurityContext: securityContext,
			},
		}
	}

	// 기존 기타 언어용 InitContainer
	return []corev1.Container{
		{
			Name:            "whatap-agent-init",
			Image:           getAgentImage(target, lang, version),
			ImagePullPolicy: corev1.PullAlways,
			VolumeMounts:    []corev1.VolumeMount{baseVolumeMount},
			SecurityContext: securityContext,
		},
	}
}

func getPythonAppConfig(envs []corev1.EnvVar) (string, string, string) {
	appName := "python-app"
	appProcessName := "python"
	OKIND := ""
	for _, env := range envs {
		if env.Name == "app_name" && env.Value != "" {
			appName = env.Value
		}
		if env.Name == "app_process_name" && env.Value != "" {
			appProcessName = env.Value
		}
		if env.Name == "OKIND" && env.Value != "" {
			OKIND = env.Value
		}
	}

	return appName, appProcessName, OKIND
}

func injectLanguageSpecificEnvVars(container corev1.Container, target monitoringv2alpha1.TargetSpec, cr monitoringv2alpha1.WhatapAgent, lang, version string, logger logr.Logger) []corev1.EnvVar {
	var envs []corev1.EnvVar
	switch lang {
	case "java":
		envs = injectJavaEnvVars(container, cr, logger)
	case "python":
		envs = injectPythonEnvVars(container, target, cr, version, logger)
	case "nodejs":
		envs = injectNodejsEnvVars(container, cr)
	case "php", "dotnet", "golang":
		envs = injectBasicKubernetesEnvVars(container)
	default:
		logger.Info("Unsupported language. Skipping env injection.", "language", lang)
		envs = container.Env
	}
	// Merge user-specified target envs without overriding existing ones
	if len(target.Envs) > 0 {
		envs = mergeEnvVars(envs, target.Envs)
	}
	return envs
}

// injectJavaEnvVars handles Java-specific environment variable injection
func injectJavaEnvVars(container corev1.Container, cr monitoringv2alpha1.WhatapAgent, logger logr.Logger) []corev1.EnvVar {
	agentOption := "-javaagent:/whatap-agent/whatap.agent.java.jar"
	envVars := injectJavaToolOptions(container.Env, agentOption, logger)

	// WHATAP_JAVA_AGENT_PATH 기본값 설정: 사용자가 지정하지 않은 경우에만 추가
	// 우선순위: 컨테이너에 이미 존재하면 그대로 유지
	hasJavaAgentPath := false
	for _, e := range container.Env {
		if e.Name == "WHATAP_JAVA_AGENT_PATH" {
			hasJavaAgentPath = true
			break
		}
	}
	if !hasJavaAgentPath {
		// envVars에 동일 키가 있다면 추가하지 않음(안전장치)
		for _, e := range envVars {
			if e.Name == "WHATAP_JAVA_AGENT_PATH" {
				hasJavaAgentPath = true
				break
			}
		}
		if !hasJavaAgentPath {
			envVars = append(envVars, corev1.EnvVar{Name: "WHATAP_JAVA_AGENT_PATH", Value: "/whatap-agent/whatap.agent.java.jar"})
		}
	}

	// Java 전용 환경변수 추가 (CR 기반)
	licenseEnv := getWhatapLicenseEnvVar(cr)
	licenseEnv.Name = "license" // Java agent expects "license" env var name

	hostEnv := getWhatapHostEnvVar(cr)
	hostEnv.Name = "whatap.server.host" // Java agent expects "whatap.server.host" env var name

	portEnv := getWhatapPortEnvVar(cr)
	portEnv.Name = "whatap.server.port"

	javaEnvVars := []corev1.EnvVar{
		licenseEnv,
		hostEnv,
		portEnv,
		{Name: "whatap.micro.enabled", Value: "true"},
		{Name: "NODE_IP", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}}},
		{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}}},
		{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
	}

	return append(envVars, javaEnvVars...)
}

func injectPythonEnvVars(container corev1.Container, target monitoringv2alpha1.TargetSpec, cr monitoringv2alpha1.WhatapAgent, version string, logger logr.Logger) []corev1.EnvVar {
	logger.Info("Configuring Python APM agent injection with whatap.conf", "version", version)

	// Read from target.Envs (align with Java approach)
	appName, appProcessName, okind := getPythonAppConfig(target.Envs)
	// Preserve previous default behavior: if appName is still default, use container name
	if appName == "" || appName == "python-app" {
		appName = container.Name
	}

	// Python 전용 환경변수 추가 (CR 기반)
	licenseEnv := getWhatapLicenseEnvVar(cr)
	licenseEnv.Name = "license" // Python agent expects "license" env var name

	hostEnv := getWhatapHostEnvVar(cr)
	hostEnv.Name = "whatap_server_host" // Python agent expects "whatap_server_host" env var name

	portEnv := getWhatapPortEnvVar(cr)
	portEnv.Name = "whatap_server_port"

	// Python APM 환경변수 구성
	envVars := []corev1.EnvVar{
		// Whatap 서버 연결 정보
		licenseEnv,
		hostEnv,
		portEnv,

		// Python 애플리케이션 정보
		{Name: "app_name", Value: appName},
		{Name: "app_process_name", Value: appProcessName},

		// Python 에이전트 경로 설정 (새로운 구조)
		{Name: "WHATAP_HOME", Value: "/whatap-agent"},
		// Whatap 설정
		{Name: "whatap.micro.enabled", Value: "true"},

		// Kubernetes 메타데이터
		{Name: "NODE_IP", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}}},
		{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}}},
		{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
	}

	// Add OKIND if provided
	if okind != "" {
		envVars = append(envVars, corev1.EnvVar{Name: "OKIND", Value: okind})
	}

	// PYTHONPATH 안전하게 주입 (새로운 구조)
	envVars = injectPythonPath(envVars, "/whatap-agent/whatap/bootstrap", logger)

	// WHATAP_PYTHON_AGENT_PATH 기본값 설정: 사용자가 지정하지 않은 경우에만 추가
	// 우선순위: 컨테이너에 이미 존재하면 그대로 유지
	hasPythonAgentPath := false
	for _, e := range container.Env {
		if e.Name == "WHATAP_PYTHON_AGENT_PATH" {
			hasPythonAgentPath = true
			break
		}
	}
	if !hasPythonAgentPath {
		// envVars에 동일 키가 있다면 추가하지 않음(이 경우도 드뭅니다만 안전장치)
		for _, e := range envVars {
			if e.Name == "WHATAP_PYTHON_AGENT_PATH" {
				hasPythonAgentPath = true
				break
			}
		}
		if !hasPythonAgentPath {
			envVars = append(envVars, corev1.EnvVar{Name: "WHATAP_PYTHON_AGENT_PATH", Value: "/whatap-agent/whatap_python"})
		}
	}

	return append(container.Env, envVars...)
}

// injectNodejsEnvVars handles Node.js-specific environment variable injection
func injectNodejsEnvVars(container corev1.Container, cr monitoringv2alpha1.WhatapAgent) []corev1.EnvVar {
	// Node.js 전용 환경변수 추가 (CR 기반)
	licenseEnv := getWhatapLicenseEnvVar(cr)
	licenseEnv.Name = "WHATAP_LICENSE" // Node.js agent expects "WHATAP_LICENSE" env var name

	hostEnv := getWhatapHostEnvVar(cr)
	hostEnv.Name = "WHATAP_SERVER_HOST" // Node.js agent expects "WHATAP_SERVER_HOST" env var name

	nodejsEnvVars := []corev1.EnvVar{
		licenseEnv,
		hostEnv,
		{Name: "WHATAP_MICRO_ENABLED", Value: "true"},
	}

	return append(container.Env, nodejsEnvVars...)
}

// injectBasicKubernetesEnvVars handles basic Kubernetes environment variables for other languages
func injectBasicKubernetesEnvVars(container corev1.Container) []corev1.EnvVar {
	basicEnvVars := []corev1.EnvVar{
		{Name: "NODE_IP", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}}},
		{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}}},
		{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
	}

	return append(container.Env, basicEnvVars...)
}

// wrapPythonCommand wraps Python application command with whatap-start-agent
func wrapPythonCommand(container *corev1.Container, logger logr.Logger) {
	if len(container.Command) > 0 || len(container.Args) > 0 {
		originalCommand := container.Command
		originalArgs := container.Args

		// whatap-start-agent 뒤에 원본 명령어와 인자들을 직접 전달
		newArgs := []string{}
		if len(originalCommand) > 0 {
			newArgs = append(newArgs, originalCommand...)
		}
		if len(originalArgs) > 0 {
			newArgs = append(newArgs, originalArgs...)
		}

		logger.Info("Wrapping Python application command with whatap-start-agent", "originalCommand", originalCommand, "originalArgs", originalArgs)

		container.Command = []string{"/whatap-agent/bin/whatap-start-agent"}
		container.Args = newArgs
	}
}

func getWhatapHostEnvVar(cr monitoringv2alpha1.WhatapAgent) corev1.EnvVar {
	host := config.GetWhatapHost()
	return corev1.EnvVar{Name: "WHATAP_HOST", Value: host}
}

func getWhatapPortEnvVar(cr monitoringv2alpha1.WhatapAgent) corev1.EnvVar {
	port := config.GetWhatapPort()
	return corev1.EnvVar{Name: "WHATAP_PORT", Value: port}
}

// Deployment 처리

// PodSpec 수정 (자동 주입 핵심 로직)
func patchPodTemplateSpec(podSpec *corev1.PodSpec, cr monitoringv2alpha1.WhatapAgent, target monitoringv2alpha1.TargetSpec, namespace string, logger logr.Logger) {
	lang := target.Language
	version := target.WhatapApmVersions[lang]

	logger.Info("Starting APM agent injection", "language", lang, "version", version, "target", target.Name)

	// Check if version is available for the language
	if version == "" {
		logger.Error(fmt.Errorf("no version specified for language %s", lang), "Missing version for language", "language", lang, "target", target.Name, "availableVersions", target.WhatapApmVersions)
		return
	}

	// 1️⃣ InitContainer - 에이전트 복사
	initContainers := createAgentInitContainers(target, cr, lang, version, logger)

	//Merge ConfigMap copy into agent init container (avoid separate alpine init)
	if target.Config.Mode == "custom" && target.Config.ConfigMapRef != nil {
		// Add ConfigMap volume to PodSpec
		podSpec.Volumes = appendIfNotExists(podSpec.Volumes, corev1.Volume{
			Name: "config-volume",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: target.Config.ConfigMapRef.Name,
					},
				},
			},
		})
		// Ensure the first init container copies the config then runs its default init script
		if len(initContainers) > 0 {
			initContainers[0].VolumeMounts = append(initContainers[0].VolumeMounts, corev1.VolumeMount{
				Name:      "config-volume",
				MountPath: "/config-volume",
			})
			initContainers[0].Command = []string{"sh", "-c"}
			initContainers[0].Args = []string{"cp /config-volume/whatap.conf /whatap-agent/ && chmod 644 /whatap-agent/whatap.conf && if [ -x /init.sh ]; then /init.sh; fi"}
		}
	}

	podSpec.InitContainers = append(podSpec.InitContainers, initContainers...)

	// 3️⃣ 공유 볼륨 추가 (에이전트용)
	podSpec.Volumes = appendIfNotExists(podSpec.Volumes, corev1.Volume{
		Name: "whatap-agent-volume",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

	// 컨테이너별 환경변수 & 볼륨 마운트
	for i, container := range podSpec.Containers {
		podSpec.Containers[i].Env = injectLanguageSpecificEnvVars(container, target, cr, lang, version, logger)

		// Python 전용: 새로운 구조에서는 sitecustomize.py를 통한 자동 활성화 사용
		// wrapPythonCommand는 더 이상 필요하지 않음 (OpenTelemetry 방식)
		if lang == "python" {
			logger.Info("Using sitecustomize.py for automatic Python APM activation, no command wrapping needed")
		}

		// 공통 볼륨 마운트
		podSpec.Containers[i].VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      "whatap-agent-volume",
			MountPath: "/whatap-agent",
		})
	}
}

// PYTHONPATH 안전하게 주입 (OpenTelemetry 방식)
func injectPythonPath(envVars []corev1.EnvVar, bootstrapPath string, logger logr.Logger) []corev1.EnvVar {
	found := false
	for i, env := range envVars {
		if env.Name == "PYTHONPATH" {
			if env.ValueFrom != nil {
				logger.Info("PYTHONPATH is set via ConfigMap/Secret. Skipping injection.")
				found = true
				break
			} else {
				// 기존 PYTHONPATH 앞에 bootstrap 경로 추가
				if env.Value == "" {
					envVars[i].Value = bootstrapPath
				} else {
					envVars[i].Value = bootstrapPath + ":" + env.Value
				}
				found = true
				break
			}
		}
	}
	if !found {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "PYTHONPATH",
			Value: bootstrapPath,
		})
	}
	return envVars
}

// JAVA_TOOL_OPTIONS 안전하게 주입
func injectJavaToolOptions(envVars []corev1.EnvVar, agentOption string, logger logr.Logger) []corev1.EnvVar {
	found := false
	for i, env := range envVars {
		if env.Name == "JAVA_TOOL_OPTIONS" {
			if env.ValueFrom != nil {
				logger.Info("JAVA_TOOL_OPTIONS is set via ConfigMap/Secret. Skipping injection.")
				found = true
				break
			} else {
				envVars[i].Value = env.Value + " " + agentOption
				found = true
				break
			}
		}
	}
	if !found {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "JAVA_TOOL_OPTIONS",
			Value: agentOption,
		})
	}
	return envVars
}

func hasLabels(labels, selector map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// matchesLabelExpressions checks if labels match the given expressions
func matchesLabelExpressions(labels map[string]string, expressions []monitoringv2alpha1.LabelSelectorRequirement) bool {
	for _, req := range expressions {
		if !matchesLabelExpression(labels, req) {
			return false
		}
	}
	return true
}

// matchesLabelExpression checks if labels match a single expression
func matchesLabelExpression(labels map[string]string, req monitoringv2alpha1.LabelSelectorRequirement) bool {
	switch req.Operator {
	case "In":
		return matchesInOperator(labels, req)
	case "NotIn":
		return matchesNotInOperator(labels, req)
	case "Exists":
		return matchesExistsOperator(labels, req)
	case "DoesNotExist":
		return matchesDoesNotExistOperator(labels, req)
	default:
		return false
	}
}

// matchesInOperator checks if label value is in the specified values
func matchesInOperator(labels map[string]string, req monitoringv2alpha1.LabelSelectorRequirement) bool {
	value, exists := labels[req.Key]
	if !exists {
		return false
	}
	for _, v := range req.Values {
		if value == v {
			return true
		}
	}
	return false
}

// matchesNotInOperator checks if label value is not in the specified values
func matchesNotInOperator(labels map[string]string, req monitoringv2alpha1.LabelSelectorRequirement) bool {
	value, exists := labels[req.Key]
	if !exists {
		return true
	}
	for _, v := range req.Values {
		if value == v {
			return false
		}
	}
	return true
}

// matchesExistsOperator checks if label exists
func matchesExistsOperator(labels map[string]string, req monitoringv2alpha1.LabelSelectorRequirement) bool {
	_, exists := labels[req.Key]
	return exists
}

// matchesDoesNotExistOperator checks if label does not exist
func matchesDoesNotExistOperator(labels map[string]string, req monitoringv2alpha1.LabelSelectorRequirement) bool {
	_, exists := labels[req.Key]
	return !exists
}

// matchesSelector checks if the given labels match the selector
func matchesSelector(labels map[string]string, selector monitoringv2alpha1.PodSelector) bool {
	// Check matchLabels
	if !hasLabels(labels, selector.MatchLabels) {
		return false
	}

	// Check matchExpressions
	return matchesLabelExpressions(labels, selector.MatchExpressions)
}

// matchesNamespaceSelector checks if the given namespace matches the selector
func matchesNamespaceSelector(namespaceName string, namespaceLabels map[string]string, selector monitoringv2alpha1.NamespaceSelector) bool {
	// Check matchNames
	if !matchesNamespaceNames(namespaceName, selector.MatchNames) {
		return false
	}

	// Check matchLabels
	if !hasLabels(namespaceLabels, selector.MatchLabels) {
		return false
	}

	// Check matchExpressions
	return matchesLabelExpressions(namespaceLabels, selector.MatchExpressions)
}

// matchesNamespaceNames checks if namespace name matches any of the specified names
func matchesNamespaceNames(namespaceName string, matchNames []string) bool {
	if len(matchNames) == 0 {
		return true
	}

	for _, name := range matchNames {
		if namespaceName == name {
			return true
		}
	}
	return false
}

// getAgentImage returns the image name to use for the agent
// If a custom image name is provided, it will be used
// Otherwise, the default image name format will be used
func getAgentImage(target monitoringv2alpha1.TargetSpec, lang, version string) string {
	if target.CustomImageName != "" {
		return target.CustomImageName
	}
	return fmt.Sprintf("public.ecr.aws/whatap/apm-init-%s:%s", lang, version)
}

func appendIfNotExists(volumes []corev1.Volume, newVol corev1.Volume) []corev1.Volume {
	for _, v := range volumes {
		if v.Name == newVol.Name {
			return volumes
		}
	}
	return append(volumes, newVol)
}

// mergeEnvVars appends extras into base without overriding existing names
func mergeEnvVars(base []corev1.EnvVar, extras []corev1.EnvVar) []corev1.EnvVar {
	existing := make(map[string]struct{}, len(base))
	for _, e := range base {
		if e.Name != "" {
			existing[e.Name] = struct{}{}
		}
	}
	for _, e := range extras {
		if e.Name == "" {
			continue
		}
		if _, ok := existing[e.Name]; ok {
			continue
		}
		base = append(base, e)
		existing[e.Name] = struct{}{}
	}
	return base
}
