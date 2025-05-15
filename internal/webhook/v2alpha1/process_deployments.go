package v2alpha1

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	"github.com/whatap/whatap-operator/internal/controller"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Deployment 처리
func processDeployments(ctx context.Context, r *controller.WhatapAgentReconciler, logger logr.Logger, ns string, target monitoringv2alpha1.TargetSpec, cr monitoringv2alpha1.WhatapAgent) {
	// 1) 네임스페이스 내 모든 Deployment 읽기
	var deployList appsv1.DeploymentList
	if err := r.List(ctx, &deployList, client.InNamespace(ns)); err != nil {
		logger.Error(err, "Failed to list Deployments")
		return
	}

	for _, deploy := range deployList.Items {
		if isAlreadyPatched(deploy.Spec.Template.Spec) {
			logger.Info("isAlreadyPatched",
				"deployName", deploy.Name,
				"deployNamespace", deploy.Namespace,
			)
			continue
		}

		// 2) 필터링: PodTemplate 라벨 / Selector 라벨 하나라도 매칭되면 대상
		sel := target.PodSelector.MatchLabels
		matchByTemplate := hasLabels(deploy.Spec.Template.Labels, sel)
		//matchBySelector := hasLabels(deploy.Spec.Selector.MatchLabels, sel)
		//matchByLabels := hasLabels(deploy.Labels, sel)
		//matchByAnnotations := hasLabels(deploy.Annotations, sel)
		if !(matchByTemplate) {
			continue
		}

		logger.Info("Detected APM injection target",
			"deployment", deploy.Name,
			"targetName", target.Name,
			"language", target.Language,
			"namespaceSelector", fmt.Sprintf("%#v", target.NamespaceSelector.MatchNames),
			"podSelector", fmt.Sprintf("%#v", target.PodSelector.MatchLabels),
			"matchByTemplate", matchByTemplate,
			//"matchBySelector", matchBySelector,
			//"matchByLabels", matchByLabels,
		)

		// 3) 패치 로직 적용
		patchPodTemplateSpec(&deploy.Spec.Template.Spec, cr, target, logger)
		if err := r.Update(ctx, &deploy); err != nil {
			// ❌ 주입 실패 로그
			logger.Error(err, "Failed to inject Whatap APM into Deployment",
				"deployment", deploy.Name, "namespace", deploy.Namespace)
		} else {
			// ✅ 주입 성공 로그
			logger.Info("Successfully injected Whatap APM into Deployment",
				"deployment", deploy.Name, "namespace", deploy.Namespace)
		}
	}
}

// PodSpec 수정 (자동 주입 핵심 로직)
func patchPodTemplateSpec(podSpec *corev1.PodSpec, cr monitoringv2alpha1.WhatapAgent, target monitoringv2alpha1.TargetSpec, logger logr.Logger) {
	lang := target.Language
	version := target.WhatapApmVersions[lang]

	// 1️⃣ InitContainer - 에이전트 복사
	initContainers := []corev1.Container{
		{
			Name:  "whatap-agent-init",
			Image: fmt.Sprintf("public.ecr.aws/whatap/apm-init-%s:%s", lang, version),
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
		initContainers = append(initContainers, corev1.Container{
			Name:    "whatap-config-init",
			Image:   "alpine:3.18",
			Command: []string{"sh", "-c"},
			Args: []string{
				fmt.Sprintf(`
					cp /config-volume/whatap.conf /whatap-agent/ && \
					echo "license=%s" >> /whatap-agent/whatap.conf && \
					echo "whatap.server.host=%s" >> /whatap-agent/whatap.conf && \
					echo "whatap.server.port=%s" >> /whatap-agent/whatap.conf && \
					echo "whatap.micro.enabled=true" >> /whatap-agent/whatap.conf
					`, cr.Spec.License, cr.Spec.Host, cr.Spec.Port),
			},
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
		initContainers = append(initContainers, corev1.Container{
			Name:    "whatap-config-init",
			Image:   "alpine:3.18",
			Command: []string{"sh", "-c"},
			Args: []string{
				fmt.Sprintf(`echo "license=%s" > /whatap-agent/whatap.conf && echo "whatap.server.host=%s" >> /whatap-agent/whatap.conf &&echo "whatap.server.port=%s" >> /whatap-agent/whatap.conf && echo "whatap.micro.enabled=true" >> /whatap-agent/whatap.conf`, cr.Spec.License, cr.Spec.Host, cr.Spec.Port),
			},
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

// 이미 패치되었는지 확인
func isAlreadyPatched(podSpec corev1.PodSpec) bool {
	for _, ic := range podSpec.InitContainers {
		if ic.Name == "whatap-agent-init" {
			return true
		}
	}
	return false
}
func hasLabels(labels, selector map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}
func appendIfNotExists(volumes []corev1.Volume, newVol corev1.Volume) []corev1.Volume {
	for _, v := range volumes {
		if v.Name == newVol.Name {
			return volumes
		}
	}
	return append(volumes, newVol)
}
