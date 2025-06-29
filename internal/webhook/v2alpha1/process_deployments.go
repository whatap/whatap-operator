package v2alpha1

import (
	"fmt"
	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
)


// Helper functions to get environment variables for Whatap credentials
// These functions use values from the CR spec, which are guaranteed to be populated

func getWhatapLicenseEnvVar(cr monitoringv2alpha1.WhatapAgent) corev1.EnvVar {
	return corev1.EnvVar{Name: "WHATAP_LICENSE", Value: cr.Spec.License}
}

func getWhatapHostEnvVar(cr monitoringv2alpha1.WhatapAgent) corev1.EnvVar {
	return corev1.EnvVar{Name: "WHATAP_HOST", Value: cr.Spec.Host}
}

func getWhatapPortEnvVar(cr monitoringv2alpha1.WhatapAgent) corev1.EnvVar {
	return corev1.EnvVar{Name: "WHATAP_PORT", Value: cr.Spec.Port}
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
	var initContainers []corev1.Container

	if lang == "python" {
		// Python APM 전용 InitContainer - OpenTelemetry 방식
		logger.Info("Using Python APM bootstrap init container", "version", version)
		initContainers = []corev1.Container{
			{
				Name:  "whatap-python-bootstrap-init",
				Image: getAgentImage(target, lang, version), // public.ecr.aws/whatap/apm-init-python:1.8.5
				Command: []string{"/init.sh"},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "whatap-agent-volume",
						MountPath: "/whatap-agent",
					},
				},
			},
		}
	} else {
		// 기존 Java 및 기타 언어용 InitContainer
		initContainers = []corev1.Container{
			{
				Name:  "whatap-agent-init",
				Image: getAgentImage(target, lang, version),
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "whatap-agent-volume",
						MountPath: "/whatap-agent",
					},
				},
			},
		}
	}

	// 2️⃣ ConfigMap 기반 config 생성 (mode가 configMapRef 때만 추가)
	if target.Config.Mode == "configMapRef" && target.Config.ConfigMapRef != nil {
		logger.Info("Using ConfigMap-based configuration", "configMapName", target.Config.ConfigMapRef.Name, "namespace", target.Config.ConfigMapRef.Namespace)
		// Build the command with basic configuration using environment variables
		command := `
				cp /config-volume/whatap.conf /whatap-agent/ && \
				echo "license=${WHATAP_LICENSE}" >> /whatap-agent/whatap.conf && \
				echo "whatap.server.host=${WHATAP_HOST}" >> /whatap-agent/whatap.conf && \
				echo "whatap.server.port=${WHATAP_PORT}" >> /whatap-agent/whatap.conf && \
				echo "whatap.micro.enabled=true" >> /whatap-agent/whatap.conf`

		// Add additional arguments if provided
		if len(target.AdditionalArgs) > 0 {
			for key, value := range target.AdditionalArgs {
				command += fmt.Sprintf(` && \
				echo "%s=%s" >> /whatap-agent/whatap.conf`, key, value)
			}
		}

		configInitContainer := corev1.Container{
			Name:    "whatap-config-init",
			Image:   "alpine:3.18",
			Command: []string{"sh", "-c"},
			Args:    []string{command},
			Env: []corev1.EnvVar{
				getWhatapLicenseEnvVar(cr),
				getWhatapHostEnvVar(cr),
				getWhatapPortEnvVar(cr),
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "whatap-agent-volume", MountPath: "/whatap-agent"},
				{Name: "config-volume", MountPath: "/config-volume"},
			},
		}
		initContainers = append(initContainers, configInitContainer)

		// ConfigMap 마운트 추가
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
	} else if lang == "java" {
		// 3️⃣ Java 기본 whatap.conf 생성 (ConfigMap 사용 안할 때)
		logger.Info("Using default Java configuration (no ConfigMap)", "language", lang)
		// Build the command with basic configuration using environment variables
		command := `echo "license=${WHATAP_LICENSE}" > /whatap-agent/whatap.conf && echo "whatap.server.host=${WHATAP_HOST}" >> /whatap-agent/whatap.conf && echo "whatap.server.port=${WHATAP_PORT}" >> /whatap-agent/whatap.conf && echo "whatap.micro.enabled=true" >> /whatap-agent/whatap.conf`

		// Add additional arguments if provided
		if len(target.AdditionalArgs) > 0 {
			for key, value := range target.AdditionalArgs {
				command += fmt.Sprintf(` && echo "%s=%s" >> /whatap-agent/whatap.conf`, key, value)
			}
		}

		configInitContainer := corev1.Container{
			Name:    "whatap-config-init",
			Image:   "alpine:3.18",
			Command: []string{"sh", "-c"},
			Args:    []string{command},
			Env: []corev1.EnvVar{
				getWhatapLicenseEnvVar(cr),
				getWhatapHostEnvVar(cr),
				getWhatapPortEnvVar(cr),
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "whatap-agent-volume", MountPath: "/whatap-agent"},
			},
		}
		initContainers = append(initContainers, configInitContainer)
	} else {
		logger.Info("No configuration mode specified, skipping config init container", "language", lang)
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
		switch lang {
		case "java":
			agentOption := "-javaagent:/whatap-agent/whatap.agent.java.jar"
			podSpec.Containers[i].Env = injectJavaToolOptions(container.Env, agentOption, logger)

 		// 🔹 Java 전용 환경변수 추가 (CR 기반)
 		licenseEnv := getWhatapLicenseEnvVar(cr)
 		licenseEnv.Name = "license" // Java agent expects "license" env var name

 		hostEnv := getWhatapHostEnvVar(cr)
 		hostEnv.Name = "whatap.server.host" // Java agent expects "whatap.server.host" env var name

 		hostPort := getWhatapPortEnvVar(cr)
 		hostPort.Name = "whatap.server.port"

			podSpec.Containers[i].Env = append(podSpec.Containers[i].Env,
				licenseEnv,
				hostEnv,
				hostPort,
				corev1.EnvVar{Name: "whatap.micro.enabled", Value: "true"},
				corev1.EnvVar{Name: "NODE_IP", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}}},
				corev1.EnvVar{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}}},
				corev1.EnvVar{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
			)
		case "python":
			logger.Info("Configuring Python APM agent injection (OpenTelemetry style)", "version", version)

			// 🔹 Python 전용 환경변수 추가 (CR 기반)
			licenseEnv := getWhatapLicenseEnvVar(cr)
			licenseEnv.Name = "license" // Python agent expects "license" env var name

			hostEnv := getWhatapHostEnvVar(cr)
			hostEnv.Name = "whatap_server_host" // Python agent expects "whatap_server_host" env var name

			podSpec.Containers[i].Env = append(container.Env,
				licenseEnv,
				hostEnv,
				corev1.EnvVar{Name: "app_name", Value: container.Name},
				corev1.EnvVar{Name: "app_process_name", Value: "python"},
				// 🔥 핵심: PYTHONPATH에 bootstrap 디렉터리 추가 - 가상환경 자동 호환!
				corev1.EnvVar{Name: "PYTHONPATH", Value: "/whatap-agent/bootstrap:$PYTHONPATH"},
				corev1.EnvVar{Name: "WHATAP_HOME", Value: "/whatap-agent"},
				// 🔥 PATH에 bin 디렉터리 추가 (네이티브 명령어 사용)
				corev1.EnvVar{Name: "PATH", Value: "/whatap-agent/bin:$PATH"},
				corev1.EnvVar{Name: "NODE_IP", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}}},
			)

			// 🔥 명령어 변경 불필요! sitecustomize.py가 자동 처리
		case "nodejs":
 		// 🔹 Node.js 전용 환경변수 추가 (CR 기반)
 		licenseEnv := getWhatapLicenseEnvVar(cr)
 		licenseEnv.Name = "WHATAP_LICENSE" // Node.js agent expects "WHATAP_LICENSE" env var name

 		hostEnv := getWhatapHostEnvVar(cr)
 		hostEnv.Name = "WHATAP_SERVER_HOST" // Node.js agent expects "WHATAP_SERVER_HOST" env var name

			podSpec.Containers[i].Env = append(container.Env,
				licenseEnv,
				hostEnv,
				corev1.EnvVar{Name: "WHATAP_MICRO_ENABLED", Value: "true"},
			)
		case "php", "dotnet", "golang":
			podSpec.Containers[i].Env = append(container.Env,
				corev1.EnvVar{Name: "NODE_IP", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}}},
				corev1.EnvVar{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}}},
				corev1.EnvVar{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
			)
		default:
			logger.Info("Unsupported language. Skipping env injection.", "language", lang)
		}

		// 공통 볼륨 마운트
		podSpec.Containers[i].VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      "whatap-agent-volume",
			MountPath: "/whatap-agent",
		})
	}
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

// matchesSelector checks if the given labels match the selector
func matchesSelector(labels map[string]string, selector monitoringv2alpha1.PodSelector) bool {
	// Check matchLabels
	if !hasLabels(labels, selector.MatchLabels) {
		return false
	}

	// Check matchExpressions
	for _, req := range selector.MatchExpressions {
		switch req.Operator {
		case "In":
			// The label must exist and its value must be in the specified values
			value, exists := labels[req.Key]
			if !exists {
				return false
			}
			found := false
			for _, v := range req.Values {
				if value == v {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		case "NotIn":
			// If the label exists, its value must not be in the specified values
			value, exists := labels[req.Key]
			if exists {
				for _, v := range req.Values {
					if value == v {
						return false
					}
				}
			}
		case "Exists":
			// The label must exist
			_, exists := labels[req.Key]
			if !exists {
				return false
			}
		case "DoesNotExist":
			// The label must not exist
			_, exists := labels[req.Key]
			if exists {
				return false
			}
		}
	}

	return true
}

// matchesNamespaceSelector checks if the given namespace matches the selector
func matchesNamespaceSelector(namespaceName string, namespaceLabels map[string]string, selector monitoringv2alpha1.NamespaceSelector) bool {
	// Check matchNames
	if len(selector.MatchNames) > 0 {
		found := false
		for _, name := range selector.MatchNames {
			if namespaceName == name {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check matchLabels
	if !hasLabels(namespaceLabels, selector.MatchLabels) {
		return false
	}

	// Check matchExpressions
	for _, req := range selector.MatchExpressions {
		switch req.Operator {
		case "In":
			// The label must exist and its value must be in the specified values
			value, exists := namespaceLabels[req.Key]
			if !exists {
				return false
			}
			found := false
			for _, v := range req.Values {
				if value == v {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		case "NotIn":
			// If the label exists, its value must not be in the specified values
			value, exists := namespaceLabels[req.Key]
			if exists {
				for _, v := range req.Values {
					if value == v {
						return false
					}
				}
			}
		case "Exists":
			// The label must exist
			_, exists := namespaceLabels[req.Key]
			if !exists {
				return false
			}
		case "DoesNotExist":
			// The label must not exist
			_, exists := namespaceLabels[req.Key]
			if exists {
				return false
			}
		}
	}

	return true
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
