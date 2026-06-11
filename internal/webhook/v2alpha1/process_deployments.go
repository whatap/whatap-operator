package v2alpha1

import (
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

	// Resolve resource requirements: Target > Instrumentation > default
	// 기본값으로 limits만 설정하여 ResourceQuota가 걸린 namespace에서도 Pod 생성이 가능하도록 함
	defaultResources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("200m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
	resources := defaultResources
	if target.InitContainerResources != nil {
		resources = *target.InitContainerResources
	} else if cr.Spec.Features.Apm.Instrumentation.InitContainerResources != nil {
		resources = *cr.Spec.Features.Apm.Instrumentation.InitContainerResources
	}

	if lang == "python" {
		logger.Info("Using Python APM bootstrap init container with new structure", "version", version)

		// Get Python app configuration
		appName, appProcessName, OKIND := getPythonAppConfig(target.Envs)

		// Prepare environment variables for Python InitContainer
		envVars := []corev1.EnvVar{
			getWhatapLicenseEnvVar(cr, target),
			getWhatapHostEnvVar(cr, target),
			getWhatapPortEnvVar(cr, target),
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
				Resources:       resources,
			},
		}
	}

	if lang == "java" {
		logger.Info("Using Java APM init container with config generation", "version", version)

		// Java 설정을 위한 환경변수 준비
		envVars := []corev1.EnvVar{
			getWhatapLicenseEnvVar(cr, target),
			getWhatapHostEnvVar(cr, target),
			getWhatapPortEnvVar(cr, target),
		}

		return []corev1.Container{
			{
				Name:            InitContainerName,
				Image:           getAgentImage(target, lang, version),
				ImagePullPolicy: corev1.PullAlways,
				Env:             envVars,
				VolumeMounts:    []corev1.VolumeMount{baseVolumeMount},
				SecurityContext: securityContext,
				Resources:       resources,
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
			Resources:       resources,
		},
	}
}

func injectLanguageSpecificEnvVars(container corev1.Container, target monitoringv2alpha1.TargetSpec, cr monitoringv2alpha1.WhatapAgent, lang, version string, logger logr.Logger) []corev1.EnvVar {
	var envs []corev1.EnvVar

	switch lang {
	case "java":
		envs = injectJavaEnvVars(container, target, cr, logger)
	case "python":
		envs = injectPythonEnvVars(container, target, cr, version, logger)
	case "nodejs":
		envs = injectNodejsEnvVars(container, target, cr)
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

	// Merge custom-config and/or plugin file copies into the agent init container
	// (avoids spawning a separate alpine init container). Both inputs are optional and
	// may be combined. The init command runs in order:
	//   1) copy custom whatap.conf into $WHATAP_HOME   (pre-init)
	//   2) the agent image's own /init.sh              (lays down the agent files)
	//   3) copy plugin files into $WHATAP_HOME/plugin/ (post-init, so they are not
	//      clobbered by the agent layout produced in step 2)
	if (target.Config.Mode == "custom" && target.Config.ConfigMapRef != nil) || target.Config.PluginConfigMapRef != nil {
		var preInitSteps, postInitSteps []string

		// 1) Custom whatap.conf via ConfigMap (expects key "whatap.conf").
		if target.Config.Mode == "custom" && target.Config.ConfigMapRef != nil {
			const confVolume = "config-volume"
			const confMountPath = "/config-volume"
			podSpec.Volumes = appendIfNotExists(podSpec.Volumes, corev1.Volume{
				Name: confVolume,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: target.Config.ConfigMapRef.Name,
						},
					},
				},
			})
			if len(initContainers) > 0 {
				initContainers[0].VolumeMounts = append(initContainers[0].VolumeMounts, corev1.VolumeMount{
					Name:      confVolume,
					MountPath: confMountPath,
				})
			}
			preInitSteps = append(preInitSteps,
				fmt.Sprintf("cp %s/whatap.conf %s/", confMountPath, MountPathWhatapAgent),
				fmt.Sprintf("chmod 644 %s/whatap.conf", MountPathWhatapAgent),
			)
		}

		// 2) Agent plugin files via ConfigMap. Every entry is copied into
		//    $WHATAP_HOME/plugin/ (e.g. TraceHelperEnd.x).
		if target.Config.PluginConfigMapRef != nil {
			const pluginVolume = "whatap-plugin-volume"
			const pluginMountPath = "/whatap-plugin"
			pluginDir := MountPathWhatapAgent + "/plugin"
			podSpec.Volumes = appendIfNotExists(podSpec.Volumes, corev1.Volume{
				Name: pluginVolume,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: target.Config.PluginConfigMapRef.Name,
						},
					},
				},
			})
			if len(initContainers) > 0 {
				initContainers[0].VolumeMounts = append(initContainers[0].VolumeMounts, corev1.VolumeMount{
					Name:      pluginVolume,
					MountPath: pluginMountPath,
				})
			}
			postInitSteps = append(postInitSteps,
				fmt.Sprintf("mkdir -p %s", pluginDir),
				// -L dereferences the ConfigMap mount's ..data symlinks so real file
				// content is copied. Guarded with "|| true" so an empty/partial
				// ConfigMap does not block the Pod from starting.
				fmt.Sprintf("cp -L %s/* %s/ 2>/dev/null || true", pluginMountPath, pluginDir),
				fmt.Sprintf("chmod 644 %s/* 2>/dev/null || true", pluginDir),
			)
		}

		// Assemble the init container command: pre-init copies → agent /init.sh → post-init copies.
		if len(initContainers) > 0 {
			steps := append([]string{}, preInitSteps...)
			steps = append(steps, "if [ -x /init.sh ]; then /init.sh; fi")
			steps = append(steps, postInitSteps...)
			initContainers[0].Command = []string{"sh", "-c"}
			initContainers[0].Args = []string{strings.Join(steps, " && ")}
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
