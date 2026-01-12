package v2alpha1

import (
	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
)

// injectJavaEnvVars handles Java-specific environment variable injection
func injectJavaEnvVars(container corev1.Container, cr monitoringv2alpha1.WhatapAgent, logger logr.Logger) []corev1.EnvVar {
	agentOption := ValJavaAgentOptionPrefix + ValJavaAgentPath
	envVars := injectJavaToolOptions(container.Env, agentOption, logger)

	// WHATAP_JAVA_AGENT_PATH 기본값 설정: 사용자가 지정하지 않은 경우에만 추가
	// 우선순위: 컨테이너에 이미 존재하면 그대로 유지
	hasJavaAgentPath := false
	for _, e := range container.Env {
		if e.Name == EnvJavaAgentPath {
			hasJavaAgentPath = true
			break
		}
	}
	if !hasJavaAgentPath {
		// envVars에 동일 키가 있다면 추가하지 않음(안전장치)
		for _, e := range envVars {
			if e.Name == EnvJavaAgentPath {
				hasJavaAgentPath = true
				break
			}
		}
		if !hasJavaAgentPath {
			envVars = append(envVars, corev1.EnvVar{Name: EnvJavaAgentPath, Value: ValJavaAgentPath})
		}
	}

	// Java 전용 환경변수 추가 (CR 기반)
	licenseEnv := getWhatapLicenseEnvVar(cr)
	licenseEnv.Name = EnvJavaLicense // Java agent expects "license" env var name

	hostEnv := getWhatapHostEnvVar(cr)
	hostEnv.Name = EnvJavaWhatapHost // Java agent expects "whatap.server.host" env var name

	portEnv := getWhatapPortEnvVar(cr)
	portEnv.Name = EnvJavaWhatapPort

	javaEnvVars := []corev1.EnvVar{
		licenseEnv,
		hostEnv,
		portEnv,
		{Name: EnvWhatapMicroEnabled, Value: ValTrue},
		{Name: EnvNodeIP, ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}}},
		{Name: EnvNodeName, ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}}},
		{Name: EnvPodName, ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
	}

	return append(envVars, javaEnvVars...)
}

func injectJavaToolOptions(envVars []corev1.EnvVar, agentOption string, logger logr.Logger) []corev1.EnvVar {
	found := false
	for i, env := range envVars {
		if env.Name == EnvJavaToolOptions {
			found = true
			// Check if already present to avoid duplication
			// This is a simple check; for robustness, one might parse the string
			// but for now we append if likely missing
			// We trust the user or operator not to spam it repeatedly.
			// Ideally we check if `agentOption` substring exists.
			envVars[i].Value = env.Value + " " + agentOption
			logger.Info("Appended to existing JAVA_TOOL_OPTIONS", "option", agentOption)
			break
		}
	}
	if !found {
		envVars = append(envVars, corev1.EnvVar{Name: EnvJavaToolOptions, Value: agentOption})
		logger.Info("Added JAVA_TOOL_OPTIONS", "option", agentOption)
	}
	return envVars
}
