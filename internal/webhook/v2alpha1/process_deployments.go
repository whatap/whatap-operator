package v2alpha1

import (
	"fmt"

	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
)

func createAgentInitContainers(target monitoringv2alpha1.TargetSpec, cr monitoringv2alpha1.WhatapAgent, lang, version string, logger logr.Logger) []corev1.Container {
	baseVolumeMount := corev1.VolumeMount{
		Name:      VolumeNameWhatapAgent,
		MountPath: MountPathWhatapAgent,
	}

	// SecurityContext for init containers
	// Default is root (backward-compatible). If CR specifies overrides, apply them.
	var securityContext *corev1.SecurityContext
	// Resolve overrides: Target > Instrumentation > default
	var (
		runAsNonRootOverride *bool
		runAsUserOverride    *int64
	)
	if target.InitContainerSecurity != nil {
		runAsNonRootOverride = target.InitContainerSecurity.RunAsNonRoot
		runAsUserOverride = target.InitContainerSecurity.RunAsUser
	}
	if runAsNonRootOverride == nil && runAsUserOverride == nil {
		if cr.Spec.Features.Apm.Instrumentation.InitContainerSecurity != nil {
			runAsNonRootOverride = cr.Spec.Features.Apm.Instrumentation.InitContainerSecurity.RunAsNonRoot
			runAsUserOverride = cr.Spec.Features.Apm.Instrumentation.InitContainerSecurity.RunAsUser
		}
	}
	if runAsNonRootOverride == nil && runAsUserOverride == nil {
		// No overrides provided: default to non-root.
		// We DO NOT set RunAsUser here to allow OpenShift SCC to assign a random UID (MustRunAsRange).
		// If running on vanilla K8s, it will fallback to the image's USER (1001).
		securityContext = &corev1.SecurityContext{
			RunAsNonRoot: boolPtr(true),
		}
	} else {
		// Use only provided fields. If RunAsNonRoot=true without RunAsUser, leave RunAsUser nil for OpenShift compatibility
		securityContext = &corev1.SecurityContext{}
		if runAsNonRootOverride != nil {
			securityContext.RunAsNonRoot = runAsNonRootOverride
		}
		if runAsUserOverride != nil {
			securityContext.RunAsUser = runAsUserOverride
		}
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
			{Name: EnvAppName, Value: appName},
			{Name: EnvAppProcessName, Value: appProcessName},
			{Name: EnvOkind, Value: OKIND},
		}

		return []corev1.Container{
			{
				Name:            InitContainerName,
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
				Name:            InitContainerName,
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
			Name:            InitContainerName,
			Image:           getAgentImage(target, lang, version),
			ImagePullPolicy: corev1.PullAlways,
			VolumeMounts:    []corev1.VolumeMount{baseVolumeMount},
			SecurityContext: securityContext,
		},
	}
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
	default:
		// Other languages might just need basic Kubernetes envs + standard whatap envs if implemented
		// For now, if not specialized, just return original + basic
		envs = container.Env
	}
	// Merge user-specified target envs without overriding existing ones
	if len(target.Envs) > 0 {
		envs = mergeEnvVars(envs, target.Envs)
	}
	return envs
}

func injectBasicKubernetesEnvVars(container corev1.Container) []corev1.EnvVar {
	// Common envs if needed (NODE_IP, etc)
	// Currently handled inside language specific functions
	return container.Env
}

// Deployment 처리

// PodSpec 수정 (자동 주입 핵심 로직)
func patchPodTemplateSpec(podSpec *corev1.PodSpec, cr monitoringv2alpha1.WhatapAgent, target monitoringv2alpha1.TargetSpec, namespace string, logger logr.Logger) {
	lang := target.Language
	version := target.WhatapApmVersions[lang]
	if version == "" {
		version = "latest"
		logger.Info("No explicit version specified; defaulting to 'latest'", "language", lang, "target", target.Name)
	}

	logger.Info("Starting APM agent injection", "language", lang, "version", version, "target", target.Name)

	// 0️⃣ Ensure imagePullSecrets for pulling APM initContainer image (append only target-provided secrets; do not merge global)
	{
		// Build a set of existing secret names to avoid duplicates
		existing := map[string]struct{}{}
		for _, s := range podSpec.ImagePullSecrets {
			existing[s.Name] = struct{}{}
		}
		added := 0
		// Append target-level secrets only (per new policy)
		if len(target.ImagePullSecrets) > 0 {
			for _, s := range target.ImagePullSecrets {
				if _, ok := existing[s.Name]; !ok {
					podSpec.ImagePullSecrets = append(podSpec.ImagePullSecrets, corev1.LocalObjectReference{Name: s.Name})
					existing[s.Name] = struct{}{}
					added++
				}
			}
		}
		if added > 0 {
			logger.Info("Appended target-level imagePullSecrets for APM initContainer image pulls", "addedSecrets", added)
		}
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
			initContainers[0].Args = []string{fmt.Sprintf("cp /config-volume/whatap.conf %s/ && chmod 644 %s/whatap.conf && if [ -x /init.sh ]; then /init.sh; fi", MountPathWhatapAgent, MountPathWhatapAgent)}
		}
	}

	podSpec.InitContainers = append(podSpec.InitContainers, initContainers...)

	// 3️⃣ 공유 볼륨 추가 (에이전트용)
	podSpec.Volumes = appendIfNotExists(podSpec.Volumes, corev1.Volume{
		Name: VolumeNameWhatapAgent,
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
			Name:      VolumeNameWhatapAgent,
			MountPath: MountPathWhatapAgent,
		})
	}
}
