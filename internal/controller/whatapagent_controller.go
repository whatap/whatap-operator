package controller

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
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

func installMasterAgent(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr monitoringv2alpha1.WhatapAgent) error {
	namespace := cr.Spec.Features.KubernetesMonitoring.KubernetesMonitoringNamespace
	if namespace == "" {
		namespace = "whatap-monitoring"
	}
	version := cr.Spec.AgentImageVersion
	if version == "" {
		version = "latest"
	}
	agentImage := fmt.Sprintf("public.ecr.aws/whatap/kube_agent:%s", version)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "whatap-master-agent",
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"name": "whatap-master-agent"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"name": "whatap-master-agent"},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "whatap",
					Containers: []corev1.Container{
						{
							Name:  "whatap-master-agent",
							Image: agentImage,
							Command: []string{
								"/bin/entrypoint.sh",
							},
							Ports: []corev1.ContainerPort{
								{ContainerPort: 6600},
							},
							Env: []corev1.EnvVar{
								{Name: "WHATAP_LICENSE", Value: cr.Spec.License},
								{Name: "WHATAP_HOST", Value: cr.Spec.Host},
								{Name: "WHATAP_PORT", Value: cr.Spec.Port},
								{
									Name: "WHATP_MEM_LIMIT",
									ValueFrom: &corev1.EnvVarSource{
										ResourceFieldRef: &corev1.ResourceFieldSelector{
											ContainerName: "whatap-master-agent",
											Resource:      "limits.memory",
										},
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "start-script-volume",
									MountPath: "/bin/entrypoint.sh",
									SubPath:   "entrypoint.sh",
									ReadOnly:  true,
								},
								{
									Name:      "whatap-config-volume",
									MountPath: "/whatap_conf",
								},
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resourceMustParse("200m"),
									corev1.ResourceMemory: resourceMustParse("350Mi"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resourceMustParse("100m"),
									corev1.ResourceMemory: resourceMustParse("300Mi"),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "start-script-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "master-start-script",
									},
									DefaultMode: int32Ptr(448), // 0700
								},
							},
						},
						{
							Name: "whatap-config-volume",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	// Deployment 생성 또는 업데이트
	err := r.Create(ctx, deploy)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		logger.Error(err, "Failed to create Whatap Master Agent Deployment")
		return err
	}
	return nil
}
func installNodeAgent(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr monitoringv2alpha1.WhatapAgent) error {
	namespace := cr.Spec.Features.KubernetesMonitoring.KubernetesMonitoringNamespace
	if namespace == "" {
		namespace = "whatap-monitoring"
	}
	version := cr.Spec.AgentImageVersion
	if version == "" {
		version = "latest"
	}
	agentImage := fmt.Sprintf("public.ecr.aws/whatap/kube_agent:%s", version)

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "whatap-node-agent",
			Namespace: namespace,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"name": "whatap-node-agent"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"name": "whatap-node-agent"},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "whatap",
					Containers: []corev1.Container{
						{
							Name:  "whatap-node-helper",
							Image: agentImage,
							Command: []string{
								"/data/agent/node/cadvisor_helper",
								"-port", "6801",
							},
							Ports: []corev1.ContainerPort{
								{Name: "helperport", ContainerPort: 6801},
							},
							Env: []corev1.EnvVar{
								{
									Name: "NODE_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
									},
								},
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resourceMustParse("200m"),
									corev1.ResourceMemory: resourceMustParse("350Mi"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resourceMustParse("100m"),
									corev1.ResourceMemory: resourceMustParse("100Mi"),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "rootfs", MountPath: "/rootfs", ReadOnly: true},
								{Name: "hostsys", MountPath: "/sys", ReadOnly: true},
								{Name: "hostdiskdevice", MountPath: "/dev/disk", ReadOnly: true},
								{Name: "containerddomainsocket", MountPath: "/run/containerd/containerd.sock"},
							},
						},
						{
							Name:  "whatap-node-agent",
							Image: agentImage,
							Command: []string{
								"/bin/entrypoint.sh",
							},
							Ports: []corev1.ContainerPort{
								{Name: "nodeport", ContainerPort: 6600},
							},
							Env: []corev1.EnvVar{
								{
									Name: "NODE_IP",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"},
									},
								},
								{
									Name: "NODE_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
									},
								},
								{Name: "WHATAP_LICENSE", Value: cr.Spec.License},
								{Name: "WHATAP_HOST", Value: cr.Spec.Host},
								{Name: "WHATAP_PORT", Value: cr.Spec.Port},
								{
									Name: "WHATP_MEM_LIMIT",
									ValueFrom: &corev1.EnvVarSource{
										ResourceFieldRef: &corev1.ResourceFieldSelector{
											ContainerName: "whatap-node-agent",
											Resource:      "limits.memory",
										},
									},
								},
								{Name: "HOST_PREFIX", Value: "/rootfs"},
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resourceMustParse("200m"),
									corev1.ResourceMemory: resourceMustParse("350Mi"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resourceMustParse("100m"),
									corev1.ResourceMemory: resourceMustParse("300Mi"),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "rootfs", MountPath: "/rootfs", ReadOnly: true},
								{Name: "start-script-volume", MountPath: "/bin/entrypoint.sh", SubPath: "entrypoint.sh", ReadOnly: true},
								{Name: "whatap-config-volume", MountPath: "/whatap_conf"},
							},
						},
					},
					InitContainers: []corev1.Container{
						{
							Name:  "whatap-node-debug",
							Image: agentImage,
							Command: []string{
								"/data/agent/tools/whatap_debugger",
								"run",
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "rootfs", MountPath: "/rootfs", ReadOnly: true},
							},
						},
					},
					Tolerations: []corev1.Toleration{
						{Key: "node-role.kubernetes.io/master", Effect: corev1.TaintEffectNoSchedule},
						{Key: "node-role.kubernetes.io/control-plane", Effect: corev1.TaintEffectNoSchedule},
					},
					Volumes: []corev1.Volume{
						{Name: "rootfs", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}},
						{Name: "hostsys", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/sys"}}},
						{Name: "hostdiskdevice", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/dev/disk"}}},
						{Name: "start-script-volume", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "node-start-script"},
							DefaultMode:          int32Ptr(448),
						}}},
						{Name: "whatap-config-volume", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{Name: "containerddomainsocket", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/run/containerd/containerd.sock"}}},
					},
				},
			},
		},
	}

	err := r.Create(ctx, ds)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		logger.Error(err, "Failed to create Whatap Node Agent DaemonSet")
		return err
	}
	return nil
}
func installGpuAgent(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr monitoringv2alpha1.WhatapAgent) error {
	return nil
}
func installApiserverMonitor(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr monitoringv2alpha1.WhatapAgent) error {
	return nil
}
func installEtcdMonitor(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr monitoringv2alpha1.WhatapAgent) error {
	return nil
}
func installSchedulerMonitor(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr monitoringv2alpha1.WhatapAgent) error {
	return nil
}
func installOpenAgent(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr monitoringv2alpha1.WhatapAgent) error {
	return nil
}

func int32Ptr(i int32) *int32 { return &i }
func resourceMustParse(q string) resource.Quantity {
	qty, _ := resource.ParseQuantity(q)
	return qty
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

func appendIfNotExists(volumes []corev1.Volume, newVol corev1.Volume) []corev1.Volume {
	for _, v := range volumes {
		if v.Name == newVol.Name {
			return volumes
		}
	}
	return append(volumes, newVol)
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
