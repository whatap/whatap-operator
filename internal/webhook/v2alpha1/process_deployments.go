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

func getWhatapLicenseEnvVar(cr monitoringv2alpha1.WhatapAgent) corev1.EnvVar {
	license := config.GetWhatapLicense()
	return corev1.EnvVar{Name: "WHATAP_LICENSE", Value: license}
}

func createAgentInitContainers(target monitoringv2alpha1.TargetSpec, cr monitoringv2alpha1.WhatapAgent, lang, version string, logger logr.Logger) []corev1.Container {
	baseVolumeMount := corev1.VolumeMount{
		Name:      "whatap-agent-volume",
		MountPath: "/whatap-agent",
	}

	if lang == "python" {
		logger.Info("Using Python APM bootstrap init container with new structure", "version", version)

		// Get Python app configuration
		appName, appProcessName := getPythonAppConfig(target.AdditionalArgs)

		// Prepare environment variables for Python InitContainer
		envVars := []corev1.EnvVar{
			getWhatapLicenseEnvVar(cr),
			getWhatapHostEnvVar(cr),
			getWhatapPortEnvVar(cr),
			{Name: "APP_NAME", Value: appName},
			{Name: "APP_PROCESS_NAME", Value: appProcessName},
		}

		return []corev1.Container{
			{
				Name:            "whatap-python-bootstrap-init",
				Image:           getAgentImage(target, lang, version),
				ImagePullPolicy: corev1.PullAlways,
				Command:         []string{"/init.sh"},
				Env:             envVars,
				VolumeMounts:    []corev1.VolumeMount{baseVolumeMount},
			},
		}
	}

	// 기존 Java 및 기타 언어용 InitContainer
	return []corev1.Container{
		{
			Name:            "whatap-agent-init",
			Image:           getAgentImage(target, lang, version),
			ImagePullPolicy: corev1.PullAlways,
			VolumeMounts:    []corev1.VolumeMount{baseVolumeMount},
		},
	}
}

func createConfigInitContainer(target monitoringv2alpha1.TargetSpec, cr monitoringv2alpha1.WhatapAgent, lang string, logger logr.Logger) (*corev1.Container, *corev1.Volume) {
	baseEnvVars := []corev1.EnvVar{
		getWhatapLicenseEnvVar(cr),
		getWhatapHostEnvVar(cr),
		getWhatapPortEnvVar(cr),
	}

	if target.Config.Mode == "configMapRef" && target.Config.ConfigMapRef != nil {
		return createConfigMapBasedContainer(target, baseEnvVars, logger)
	}

	switch lang {
	case "java":
		return createJavaConfigContainer(target, baseEnvVars, logger), nil
	case "python":
		// Python uses new structure with config generated in InitContainer
		logger.Info("Python uses new structure with config generated in InitContainer, skipping config init container", "language", lang)
		return nil, nil
	default:
		logger.Info("No configuration mode specified, skipping config init container", "language", lang)
		return nil, nil
	}
}

// createConfigMapBasedContainer creates config container for ConfigMap mode
func createConfigMapBasedContainer(target monitoringv2alpha1.TargetSpec, baseEnvVars []corev1.EnvVar, logger logr.Logger) (*corev1.Container, *corev1.Volume) {
	logger.Info("Using ConfigMap-based configuration", "configMapName", target.Config.ConfigMapRef.Name, "namespace", target.Config.ConfigMapRef.Namespace)

	command := buildConfigCommand("cp /config-volume/whatap.conf /whatap-agent/ && ", target.AdditionalArgs)

	container := &corev1.Container{
		Name:            "whatap-config-init",
		Image:           "alpine:3.18",
		ImagePullPolicy: corev1.PullAlways,
		Command:         []string{"sh", "-c"},
		Args:            []string{command},
		Env:             baseEnvVars,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "whatap-agent-volume", MountPath: "/whatap-agent"},
			{Name: "config-volume", MountPath: "/config-volume"},
		},
	}

	volume := &corev1.Volume{
		Name: "config-volume",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: target.Config.ConfigMapRef.Name,
				},
			},
		},
	}

	return container, volume
}

// createJavaConfigContainer creates config container for Java
func createJavaConfigContainer(target monitoringv2alpha1.TargetSpec, baseEnvVars []corev1.EnvVar, logger logr.Logger) *corev1.Container {
	logger.Info("Using default Java configuration (no ConfigMap)", "language", "java")

	command := buildConfigCommand("", target.AdditionalArgs)

	return &corev1.Container{
		Name:            "whatap-config-init",
		Image:           "alpine:3.18",
		ImagePullPolicy: corev1.PullAlways,
		Command:         []string{"sh", "-c"},
		Args:            []string{command},
		Env:             baseEnvVars,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "whatap-agent-volume", MountPath: "/whatap-agent"},
		},
	}
}

// createPythonConfigContainer creates config container for Python
func createPythonConfigContainer(target monitoringv2alpha1.TargetSpec, baseEnvVars []corev1.EnvVar, logger logr.Logger) *corev1.Container {
	logger.Info("Using Python configuration with whatap.conf", "language", "python")

	appName, appProcessName := getPythonAppConfig(target.AdditionalArgs)
	command := buildPythonConfigCommand(target.AdditionalArgs)

	envVars := append(baseEnvVars,
		corev1.EnvVar{Name: "APP_NAME", Value: appName},
		corev1.EnvVar{Name: "APP_PROCESS_NAME", Value: appProcessName},
	)

	return &corev1.Container{
		Name:            "whatap-python-config-init",
		Image:           "alpine:3.18",
		ImagePullPolicy: corev1.PullAlways,
		Command:         []string{"sh", "-c"},
		Args:            []string{command},
		Env:             envVars,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "whatap-agent-volume", MountPath: "/whatap-agent"},
		},
	}
}

// buildConfigCommand builds the configuration command with additional args
func buildConfigCommand(prefix string, additionalArgs map[string]string) string {
	command := prefix + `echo "license=${WHATAP_LICENSE}" > /whatap-agent/whatap.conf && echo "whatap.server.host=${WHATAP_HOST}" >> /whatap-agent/whatap.conf && echo "whatap.server.port=${WHATAP_PORT}" >> /whatap-agent/whatap.conf && echo "whatap.micro.enabled=true" >> /whatap-agent/whatap.conf`

	for key, value := range additionalArgs {
		command += fmt.Sprintf(` && echo "%s=%s" >> /whatap-agent/whatap.conf`, key, value)
	}

	return command
}

// buildPythonConfigCommand builds Python-specific configuration command
func buildPythonConfigCommand(additionalArgs map[string]string) string {
	command := `echo "license=${WHATAP_LICENSE}" > /whatap-agent/whatap.conf && echo "whatap.server.host=${WHATAP_HOST}" >> /whatap-agent/whatap.conf && echo "whatap.server.port=${WHATAP_PORT}" >> /whatap-agent/whatap.conf`
	command += ` && echo "app_name=${APP_NAME}" >> /whatap-agent/whatap.conf`
	command += ` && echo "app_process_name=${APP_PROCESS_NAME}" >> /whatap-agent/whatap.conf`

	for key, value := range additionalArgs {
		if key != "app_name" && key != "app_process_name" && key != "OKIND" {
			command += fmt.Sprintf(` && echo "%s=%s" >> /whatap-agent/whatap.conf`, key, value)
		}
	}

	return command
}

// getPythonAppConfig extracts Python app configuration from additional args
func getPythonAppConfig(additionalArgs map[string]string) (string, string) {
	appName := "python-app"
	appProcessName := "python"

	if additionalArgs != nil {
		if val, exists := additionalArgs["app_name"]; exists {
			appName = val
		}
		if val, exists := additionalArgs["app_process_name"]; exists {
			appProcessName = val
		}
	}

	return appName, appProcessName
}

// injectLanguageSpecificEnvVars injects environment variables based on language
func injectLanguageSpecificEnvVars(container corev1.Container, target monitoringv2alpha1.TargetSpec, cr monitoringv2alpha1.WhatapAgent, lang, version string, logger logr.Logger) []corev1.EnvVar {
	switch lang {
	case "java":
		return injectJavaEnvVars(container, cr, logger)
	case "python":
		return injectPythonEnvVars(container, target, cr, version, logger)
	case "nodejs":
		return injectNodejsEnvVars(container, cr)
	case "php", "dotnet", "golang":
		return injectBasicKubernetesEnvVars(container)
	default:
		logger.Info("Unsupported language. Skipping env injection.", "language", lang)
		return container.Env
	}
}

// injectJavaEnvVars handles Java-specific environment variable injection
func injectJavaEnvVars(container corev1.Container, cr monitoringv2alpha1.WhatapAgent, logger logr.Logger) []corev1.EnvVar {
	agentOption := "-javaagent:/whatap-agent/whatap.agent.java.jar"
	envVars := injectJavaToolOptions(container.Env, agentOption, logger)

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

// injectPythonEnvVars handles Python-specific environment variable injection
func injectPythonEnvVars(container corev1.Container, target monitoringv2alpha1.TargetSpec, cr monitoringv2alpha1.WhatapAgent, version string, logger logr.Logger) []corev1.EnvVar {
	logger.Info("Configuring Python APM agent injection with whatap.conf", "version", version)

	appName, appProcessName, okind := getPythonEnvConfig(container.Name, target.AdditionalArgs)

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
		{Name: "WHATAP_HOME", Value: "/whatap-agent/whatap_home"},

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
	envVars = injectPythonPath(envVars, "/whatap-agent/python-apm/whatap/bootstrap", logger)

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

// getPythonEnvConfig extracts Python environment configuration from additional args
func getPythonEnvConfig(containerName string, additionalArgs map[string]string) (string, string, string) {
	appName := containerName   // default to container name
	appProcessName := "python" // default value
	okind := ""                // optional

	if additionalArgs != nil {
		if val, exists := additionalArgs["app_name"]; exists {
			appName = val
		}
		if val, exists := additionalArgs["app_process_name"]; exists {
			appProcessName = val
		}
		if val, exists := additionalArgs["OKIND"]; exists {
			okind = val
		}
	}

	return appName, appProcessName, okind
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

	// 2️⃣ Configuration init container 추가
	configContainer, configVolume := createConfigInitContainer(target, cr, lang, logger)
	if configContainer != nil {
		initContainers = append(initContainers, *configContainer)
		if configVolume != nil {
			podSpec.Volumes = appendIfNotExists(podSpec.Volumes, *configVolume)
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
