package v2alpha1

import (
	"fmt"
	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
)

// Deployment 처리

// PodSpec 수정 (자동 주입 핵심 로직)
func patchPodTemplateSpec(podSpec *corev1.PodSpec, cr monitoringv2alpha1.WhatapAgent, target monitoringv2alpha1.TargetSpec, logger logr.Logger) {
	lang := target.Language
	version := target.WhatapApmVersions[lang]

	// 1️⃣ InitContainer - 에이전트 복사
	initContainers := []corev1.Container{
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

	// 2️⃣ ConfigMap 기반 config 생성 (mode가 configMapRef 때만 추가)
	if target.Config.Mode == "configMapRef" && target.Config.ConfigMapRef != nil {
		// Build the command with basic configuration
		command := fmt.Sprintf(`
					cp /config-volume/whatap.conf /whatap-agent/ && \
					echo "license=%s" >> /whatap-agent/whatap.conf && \
					echo "whatap.server.host=%s" >> /whatap-agent/whatap.conf && \
					echo "whatap.server.port=%s" >> /whatap-agent/whatap.conf && \
					echo "whatap.micro.enabled=true" >> /whatap-agent/whatap.conf`, 
					cr.Spec.License, cr.Spec.Host, cr.Spec.Port)

		// Add additional arguments if provided
		if len(target.AdditionalArgs) > 0 {
			for key, value := range target.AdditionalArgs {
				command += fmt.Sprintf(` && \
					echo "%s=%s" >> /whatap-agent/whatap.conf`, key, value)
			}
		}

		initContainers = append(initContainers, corev1.Container{
			Name:    "whatap-config-init",
			Image:   "alpine:3.18",
			Command: []string{"sh", "-c"},
			Args: []string{command},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "whatap-agent-volume", MountPath: "/whatap-agent"},
				{Name: "config-volume", MountPath: "/config-volume"},
			},
		})

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
		// Build the command with basic configuration
		command := fmt.Sprintf(`echo "license=%s" > /whatap-agent/whatap.conf && echo "whatap.server.host=%s" >> /whatap-agent/whatap.conf && echo "whatap.server.port=%s" >> /whatap-agent/whatap.conf && echo "whatap.micro.enabled=true" >> /whatap-agent/whatap.conf`, cr.Spec.License, cr.Spec.Host, cr.Spec.Port)

		// Add additional arguments if provided
		if len(target.AdditionalArgs) > 0 {
			for key, value := range target.AdditionalArgs {
				command += fmt.Sprintf(` && echo "%s=%s" >> /whatap-agent/whatap.conf`, key, value)
			}
		}

		initContainers = append(initContainers, corev1.Container{
			Name:    "whatap-config-init",
			Image:   "alpine:3.18",
			Command: []string{"sh", "-c"},
			Args: []string{command},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "whatap-agent-volume", MountPath: "/whatap-agent"},
			},
		})
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

			// 🔹 Java 전용 환경변수 추가
			podSpec.Containers[i].Env = append(podSpec.Containers[i].Env,
				corev1.EnvVar{Name: "license", Value: cr.Spec.License},
				corev1.EnvVar{Name: "whatap.server.host", Value: cr.Spec.Host},
				corev1.EnvVar{Name: "whatap.micro.enabled", Value: "true"},
				corev1.EnvVar{Name: "NODE_IP", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}}},
				corev1.EnvVar{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}}},
				corev1.EnvVar{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
			)
		case "python":
			podSpec.Containers[i].Env = append(container.Env,
				corev1.EnvVar{Name: "license", Value: cr.Spec.License},
				corev1.EnvVar{Name: "whatap_server_host", Value: cr.Spec.Host},
				corev1.EnvVar{Name: "app_name", Value: container.Name},
				corev1.EnvVar{Name: "NODE_IP", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}}},
			)
		case "nodejs":
			podSpec.Containers[i].Env = append(container.Env,
				corev1.EnvVar{Name: "WHATAP_LICENSE", Value: cr.Spec.License},
				corev1.EnvVar{Name: "WHATAP_SERVER_HOST", Value: cr.Spec.Host},
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
