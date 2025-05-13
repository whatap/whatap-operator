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
)

func int32Ptr(i int32) *int32 { return &i }
func resourceMustParse(q string) resource.Quantity {
	qty, _ := resource.ParseQuantity(q)
	return qty
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
