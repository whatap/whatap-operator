package controller

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const whatapFinalizer = "whatapagent.finalizers.monitoring.whatap.com"

// WhatapAgentReconciler reconciles a WhatapAgent object
type WhatapAgentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *WhatapAgentReconciler) cleanupAgents(ctx context.Context, cr *monitoringv2alpha1.WhatapAgent) error {
	// ex) whatap-master-agent Deployment 삭제
	_ = r.Delete(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-master-agent", Namespace: "whatap-monitoring"},
	})
	_ = r.Delete(ctx, &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-node-agent", Namespace: "whatap-monitoring"},
	})
	// node-agent DaemonSet, GPU, api-server, etcd, scheduler, openAgent 등도 모두 Delete
	// ignore NotFound 에러
	return nil
}

// 헬퍼: 슬라이스에서 문자열 제거
func removeString(slice []string, s string) []string {
	result := []string{}
	for _, v := range slice {
		if v != s {
			result = append(result, v)
		}
	}
	return result
}

// Reconcile
func (r *WhatapAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	var cr monitoringv2alpha1.WhatapAgent

	var whatapAgent monitoringv2alpha1.WhatapAgent
	if err := r.Get(ctx, req.NamespacedName, &whatapAgent); err != nil {
		logger.Error(err, "Failed to get WhatapAgent CR")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("Reconciling WhatapAgent", "Name", whatapAgent.Name)

	// 1) Deletion 감지
	if !cr.ObjectMeta.DeletionTimestamp.IsZero() {
		// 1-1) cleanup: CR가 install한 리소스들 삭제
		if err := r.cleanupAgents(ctx, &cr); err != nil {
			return ctrl.Result{}, err
		}
		// 1-2) finalizer 제거
		cr.ObjectMeta.Finalizers = removeString(cr.ObjectMeta.Finalizers, whatapFinalizer)
		if err := r.Update(ctx, &cr); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// --- 1. Auto-Instrumentation 기존 처리 ---
	for _, target := range whatapAgent.Spec.Features.Apm.Instrumentation.Targets {
		if target.Enabled != "true" {
			continue
		}

		for _, ns := range target.NamespaceSelector.MatchNames {
			processDeployments(ctx, r, logger, ns, target, whatapAgent)
		}
	}

	// --- 2. Kubernetes Monitoring 신규 추가 ---
	kubeMon := whatapAgent.Spec.Features.KubernetesMonitoring

	if kubeMon.MasterAgentEnabled == "true" {
		logger.Info("Installing Whatap Master Agent")
		err := installMasterAgent(ctx, r, logger, whatapAgent)
		if err != nil {
			logger.Error(err, "Failed to install Master Agent")
		}
	}

	if kubeMon.NodeAgentEnabled == "true" {
		logger.Info("Installing Whatap Node Agent")
		err := installNodeAgent(ctx, r, logger, whatapAgent)
		if err != nil {
			logger.Error(err, "Failed to install Node Agent")
		}
	}

	if kubeMon.GpuEnabled == "true" {
		logger.Info("Installing GPU Monitoring Agent")
		err := installGpuAgent(ctx, r, logger, whatapAgent)
		if err != nil {
			logger.Error(err, "Failed to install GPU Agent")
		}
	}
	if kubeMon.ApiserverEnabled == "true" {
		logger.Info("Installing Apiserver Monitoring Agent")
		err := installApiserverMonitor(ctx, r, logger, whatapAgent)
		if err != nil {
			logger.Error(err, "Failed to install Apiserver Monitor")
		}
	}
	if kubeMon.EtcdEnabled == "true" {
		logger.Info("Installing Etcd Monitoring Agent")
		err := installEtcdMonitor(ctx, r, logger, whatapAgent)
		if err != nil {
			logger.Error(err, "Failed to install Etcd Monitor")
		}
	}
	if kubeMon.SchedulerEnabled == "true" {
		logger.Info("Installing Scheduler Monitoring Agent")
		err := installSchedulerMonitor(ctx, r, logger, whatapAgent)
		if err != nil {
			logger.Error(err, "Failed to install Scheduler Monitor")
		}
	}
	if kubeMon.OpenAgentEnabled == "true" {
		logger.Info("Installing Open Agent")
		err := installOpenAgent(ctx, r, logger, whatapAgent)
		if err != nil {
			logger.Error(err, "Failed to install Open Agent")
		}
	}
	return ctrl.Result{}, nil
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

// Deployment 처리
func processDeployments(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, ns string, target monitoringv2alpha1.TargetSpec, cr monitoringv2alpha1.WhatapAgent) {
	// 1) 네임스페이스 내 모든 Deployment 읽기
	var deployList appsv1.DeploymentList
	if err := r.List(ctx, &deployList, client.InNamespace(ns)); err != nil {
		logger.Error(err, "Failed to list Deployments")
		return
	}

	for _, deploy := range deployList.Items {
		if isAlreadyPatched(deploy.Spec.Template.Spec) {
			logger.Info("Deployment", deploy.Name,
				"Namespace", deploy.Namespace)
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

func (r *WhatapAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	evtLog := log.Log.WithName("WhatapAgent")

	return ctrl.NewControllerManagedBy(mgr).
		// 1) Watch the cluster-scoped WhatapAgent so CR changes still reconcile
		For(&monitoringv2alpha1.WhatapAgent{}).

		// 2) Also watch Deployments, but only Create + GenerationChanged events
		Watches(
			// <– here, just pass a Deployment object
			&appsv1.Deployment{},
			// map every matching Deployment event into a single CR reconcile request
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				dep := obj.(*appsv1.Deployment)
				evtLog.Info("Deployment event, enqueueing CR reconcile",
					"ns", dep.Namespace, "deployment", dep.Name,
				)
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{Name: "whatap"},
				}}
			}),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return true
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return true
				},
				DeleteFunc:  func(e event.DeleteEvent) bool { return false },
				GenericFunc: func(e event.GenericEvent) bool { return false },
			}),
		).
		Complete(r)
}
