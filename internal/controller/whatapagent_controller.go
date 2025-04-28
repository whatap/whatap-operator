package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
)

// WhatapAgentReconciler reconciles a WhatapAgent object
type WhatapAgentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile
func (r *WhatapAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var whatapAgent monitoringv2alpha1.WhatapAgent
	if err := r.Get(ctx, req.NamespacedName, &whatapAgent); err != nil {
		logger.Error(err, "Failed to get WhatapAgent CR")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("Reconciling WhatapAgent", "Name", whatapAgent.Name)

	for _, target := range whatapAgent.Spec.Features.Apm.Instrumentation.Targets {
		if target.Enabled != "true" {
			continue
		}

		for _, ns := range target.NamespaceSelector.MatchNames {
			processDeployments(ctx, r, logger, ns, target, whatapAgent)
		}
	}
	return ctrl.Result{}, nil
}

// Deployment 처리
func processDeployments(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, ns string, target monitoringv2alpha1.TargetSpec, cr monitoringv2alpha1.WhatapAgent) {
	var deployList appsv1.DeploymentList
	if err := r.List(ctx, &deployList, client.InNamespace(ns), client.MatchingLabels(target.PodSelector.MatchLabels)); err != nil {
		logger.Error(err, "Failed to list Deployments")
		return
	}

	for _, deploy := range deployList.Items {
		if isAlreadyPatched(deploy.Spec.Template.Spec) {
			continue
		}
		patchPodTemplateSpec(&deploy.Spec.Template.Spec, cr, target, logger)
		_ = r.Update(ctx, &deploy)
	}
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

// PodSpec 수정 (자동 주입 핵심 로직)
func patchPodTemplateSpec(podSpec *corev1.PodSpec, cr monitoringv2alpha1.WhatapAgent, target monitoringv2alpha1.TargetSpec, logger logr.Logger) {
	lang := target.Language
	version := target.WhatapApmVersions[lang]

	// InitContainer 추가
	initContainer := corev1.Container{
		Name:  "whatap-agent-init",
		Image: fmt.Sprintf("public.ecr.aws/whatap/apm-init-%s:%s", lang, version),
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "whatap-agent-volume",
				MountPath: "/whatap-agent",
			},
		},
	}
	podSpec.InitContainers = append(podSpec.InitContainers, initContainer)

	// 공유 볼륨 추가
	volumeExists := false
	for _, vol := range podSpec.Volumes {
		if vol.Name == "whatap-agent-volume" {
			volumeExists = true
			break
		}
	}
	if !volumeExists {
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: "whatap-agent-volume",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

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

// SetupWithManager
func (r *WhatapAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv2alpha1.WhatapAgent{}).
		Complete(r)
}
