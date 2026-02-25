package v2alpha1

import (
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
)

// injectNodejsEnvVars handles Node.js-specific environment variable injection
func injectNodejsEnvVars(container corev1.Container, target monitoringv2alpha1.TargetSpec, cr monitoringv2alpha1.WhatapAgent) []corev1.EnvVar {
	// Node.js 전용 환경변수 추가 (CR 기반, target envs 오버라이드 지원)
	licenseEnv := getWhatapLicenseEnvVar(cr, target)
	licenseEnv.Name = EnvNodeLicense // Node.js agent expects "WHATAP_LICENSE" env var name

	hostEnv := getWhatapHostEnvVar(cr, target)
	hostEnv.Name = EnvNodeWhatapHost // Node.js agent expects "WHATAP_SERVER_HOST" env var name

	portEnv := getWhatapPortEnvVar(cr, target)
	portEnv.Name = EnvNodeWhatapPort // Node.js agent expects "WHATAP_SERVER_PORT" env var name

	nodejsEnvVars := []corev1.EnvVar{
		licenseEnv,
		hostEnv,
		portEnv,
		{Name: EnvWhatapMicroEnabled, Value: ValTrue},
		{Name: EnvNodeIP, ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}}},
		{Name: EnvNodeName, ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}}},
		{Name: EnvPodName, ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
	}

	return append(container.Env, nodejsEnvVars...)
}
