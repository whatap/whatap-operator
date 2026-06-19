package v2alpha1

import (
	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
)

func injectNodejsEnvVars(container corev1.Container, target monitoringv2alpha1.TargetSpec, cr monitoringv2alpha1.WhatapAgent, version string, logger logr.Logger) []corev1.EnvVar {
	logger.Info("Configuring Node.js APM agent injection", "version", version)

	// Read from target.Envs (align with Python/Java approach)
	appName, appProcessName, okind := getNodejsAppConfig(target.Envs)
	if appName == "" {
		appName = container.Name
	}

	// Node.js 전용 환경변수 (CR 기반, target envs 오버라이드 지원)
	licenseEnv := getWhatapLicenseEnvVar(cr, target)
	licenseEnv.Name = EnvNodejsLicense

	hostEnv := getWhatapHostEnvVar(cr, target)
	hostEnv.Name = EnvNodejsWhatapHost

	portEnv := getWhatapPortEnvVar(cr, target)
	portEnv.Name = EnvNodejsWhatapPort

	// Node.js APM 환경변수 구성
	envVars := []corev1.EnvVar{
		// Whatap 서버 연결 정보
		licenseEnv,
		hostEnv,
		portEnv,

		// Node.js 애플리케이션 정보
		{Name: EnvAppName, Value: appName},
		{Name: EnvAppProcessName, Value: appProcessName},

		// Node.js 에이전트 경로 설정
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

	// NODEJS_PATH 안전하게 주입 (모듈 검색 경로 추가)
	envVars = injectNodejsPath(envVars, ValNodejsModules, logger)

	// NODEJS_OPTIONS 안전하게 주입 (-r whatap)
	envVars = injectNodejsOptions(envVars, ValNodejsRequire, logger)

	// WHATAP_NODEJS_AGENT_PATH 기본값 설정: 사용자가 지정하지 않은 경우에만 추가
	hasNodeAgentPath := false
	for _, e := range container.Env {
		if e.Name == EnvNodejsAgentPath {
			hasNodeAgentPath = true
			break
		}
	}
	if !hasNodeAgentPath {
		for _, e := range envVars {
			if e.Name == EnvNodejsAgentPath {
				hasNodeAgentPath = true
				break
			}
		}
		if !hasNodeAgentPath {
			envVars = append(envVars, corev1.EnvVar{Name: EnvNodejsAgentPath, Value: ValNodejsAgentPath})
		}
	}

	// 와탭 소유 연결/설정 ENV는 기존 동일 키가 있어도 operator 값으로 강제 override (KAZAA-641, Java/Python과 동일 패턴).
	// app_name/NODE_PATH/NODE_OPTIONS/agent path 등은 기존/사용자 값을 보존한다.
	return combineEnvVars(container.Env, envVars, func(name string) bool {
		_, ok := nodejsForceEnvNames[name]
		return ok
	})
}

// NODEJS_PATH 안전하게 주입 (Python의 injectPythonPath와 동일 패턴)
func injectNodejsPath(envVars []corev1.EnvVar, modulesPath string, logger logr.Logger) []corev1.EnvVar {
	found := false
	for i, env := range envVars {
		if env.Name == EnvNodejsPath {
			if env.ValueFrom != nil {
				logger.Info("NODEJS_PATH is set via ConfigMap/Secret. Skipping injection.")
				found = true
				break
			}
			logger.Info("Prepending to existing NODEJS_PATH", "original", env.Value)
			envVars[i].Value = modulesPath + ":" + env.Value
			found = true
			break
		}
	}
	if !found {
		envVars = append(envVars, corev1.EnvVar{Name: EnvNodejsPath, Value: modulesPath})
	}
	return envVars
}

// NODEJS_OPTIONS 안전하게 주입 (-r whatap)
func injectNodejsOptions(envVars []corev1.EnvVar, requireOption string, logger logr.Logger) []corev1.EnvVar {
	found := false
	for i, env := range envVars {
		if env.Name == EnvNodejsOptions {
			if env.ValueFrom != nil {
				logger.Info("NODEJS_OPTIONS is set via ConfigMap/Secret. Skipping injection.")
				found = true
				break
			}
			logger.Info("Prepending to existing NODEJS_OPTIONS", "original", env.Value)
			envVars[i].Value = requireOption + " " + env.Value
			found = true
			break
		}
	}
	if !found {
		envVars = append(envVars, corev1.EnvVar{Name: EnvNodejsOptions, Value: requireOption})
	}
	return envVars
}

func getNodejsAppConfig(envs []corev1.EnvVar) (string, string, string) {
	appName := ""
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
