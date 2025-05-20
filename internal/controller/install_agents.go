package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	"github.com/whatap/whatap-operator/internal/gpu"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ---------- 유틸 함수 ----------

func int32Ptr(i int32) *int32 { return &i }
func int64Ptr(i int64) *int64 { return &i }
func boolPtr(b bool) *bool    { return &b }
func resourceMustParse(q string) resource.Quantity {
	qty, _ := resource.ParseQuantity(q)
	return qty
}

// 리소스 기본값 세팅
func setDefaultResource(res *corev1.ResourceRequirements, defReq, defLim corev1.ResourceList) {
	if res.Requests == nil {
		res.Requests = corev1.ResourceList{}
	}
	if res.Limits == nil {
		res.Limits = corev1.ResourceList{}
	}
	for k, v := range defReq {
		if _, ok := res.Requests[k]; !ok {
			res.Requests[k] = v
		}
	}
	for k, v := range defLim {
		if _, ok := res.Limits[k]; !ok {
			res.Limits[k] = v
		}
	}
}

// 결과 메시지 helper
func logResult(logger logr.Logger, what, target string, op controllerutil.OperationResult) {
	var verb string
	switch op {
	case controllerutil.OperationResultCreated:
		verb = "created"
	case controllerutil.OperationResultUpdated:
		verb = "updated"
	default:
		verb = "unchanged"
	}
	logger.Info(fmt.Sprintf("%s %s has been %s.", what, target, verb))
}

// ---------- 주요 리소스 배포 함수 ----------

func createOrUpdateMasterAgent(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr *monitoringv2alpha1.WhatapAgent) error {
	ver := cr.Spec.Features.K8sAgent.AgentImageVersion
	if ver == "" {
		ver = "latest"
	}
	img := fmt.Sprintf("public.ecr.aws/whatap/kube_agent:%s", ver)

	resources := cr.Spec.Features.K8sAgent.MasterAgent.Resources.DeepCopy()
	setDefaultResource(resources,
		corev1.ResourceList{
			corev1.ResourceCPU:    resourceMustParse("100m"),
			corev1.ResourceMemory: resourceMustParse("300Mi")},
		corev1.ResourceList{
			corev1.ResourceCPU:    resourceMustParse("200m"),
			corev1.ResourceMemory: resourceMustParse("350Mi")},
	)

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "whatap-master-agent",
			Namespace: r.DefaultNamespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		deploy.Spec = getMasterAgentDeploymentSpec(img, resources, cr)
		return nil
	})
	if err != nil {
		logger.Error(err, "Fail create/update Whatap Master Agent Deployment")
		return err
	}
	logResult(logger, "Whatap", "Master Agent Deployment", op)
	return nil
}

func getMasterAgentDeploymentSpec(image string, res *corev1.ResourceRequirements, cr *monitoringv2alpha1.WhatapAgent) appsv1.DeploymentSpec {
	return appsv1.DeploymentSpec{
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
						Name:    "whatap-master-agent",
						Image:   image,
						Command: []string{"/bin/entrypoint.sh"},
						Ports:   []corev1.ContainerPort{{ContainerPort: 6600}},
						Env: []corev1.EnvVar{
							{Name: "WHATAP_LICENSE", Value: cr.Spec.License},
							{Name: "WHATAP_HOST", Value: cr.Spec.Host},
							{Name: "WHATAP_PORT", Value: cr.Spec.Port},
							{
								Name: "WHATAP_MEM_LIMIT",
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
						Resources: *res,
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "start-script-volume",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "master-start-script"},
								DefaultMode:          int32Ptr(0700),
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
	}
}

func createOrUpdateNodeAgent(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr *monitoringv2alpha1.WhatapAgent) error {
	ver := cr.Spec.Features.K8sAgent.AgentImageVersion
	if ver == "" {
		ver = "latest"
	}
	img := fmt.Sprintf("public.ecr.aws/whatap/kube_agent:%s", ver)

	resources := cr.Spec.Features.K8sAgent.NodeAgent.Resources.DeepCopy()
	setDefaultResource(resources,
		corev1.ResourceList{
			corev1.ResourceCPU:    resourceMustParse("100m"),
			corev1.ResourceMemory: resourceMustParse("300Mi")},
		corev1.ResourceList{
			corev1.ResourceCPU:    resourceMustParse("200m"),
			corev1.ResourceMemory: resourceMustParse("350Mi")},
	)

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "whatap-node-agent",
			Namespace: r.DefaultNamespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, ds, func() error {
		ds.Spec = getNodeAgentDaemonSetSpec(img, resources, cr)
		podSpec := &ds.Spec.Template.Spec
		if cr.Spec.Features.K8sAgent.GpuMonitoring.Enabled {
			addDcgmExporterToNodeAgent(podSpec)
		}
		return nil
	})
	if err != nil {
		logger.Error(err, "Fail create/update Whatap Node Agent DaemonSet")
		return err
	}
	logResult(logger, "Whatap", "Node Agent DaemonSet", op)
	return nil
}

func getNodeAgentDaemonSetSpec(image string, res *corev1.ResourceRequirements, cr *monitoringv2alpha1.WhatapAgent) appsv1.DaemonSetSpec {
	return appsv1.DaemonSetSpec{
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
						Name:    "whatap-node-helper",
						Image:   image,
						Command: []string{"/data/agent/node/cadvisor_helper", "-port", "6801"},
						Ports:   []corev1.ContainerPort{{Name: "helperport", ContainerPort: 6801}},
						Env: []corev1.EnvVar{
							{
								Name: "NODE_NAME",
								ValueFrom: &corev1.EnvVarSource{
									FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
								},
							},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resourceMustParse("100Mi"),
								corev1.ResourceCPU:    resourceMustParse("100m"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resourceMustParse("350Mi"),
								corev1.ResourceCPU:    resourceMustParse("200m"),
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
						Name:    "whatap-node-agent",
						Image:   image,
						Command: []string{"/bin/entrypoint.sh"},
						Ports:   []corev1.ContainerPort{{Name: "nodeport", ContainerPort: 6600}},
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
								Name: "WHATAP_MEM_LIMIT",
								ValueFrom: &corev1.EnvVarSource{
									ResourceFieldRef: &corev1.ResourceFieldSelector{
										ContainerName: "whatap-node-agent",
										Resource:      "limits.memory",
									},
								},
							},
							{Name: "HOST_PREFIX", Value: "/rootfs"},
						},
						Resources: *res,
						VolumeMounts: []corev1.VolumeMount{
							{Name: "rootfs", MountPath: "/rootfs", ReadOnly: true},
							{Name: "start-script-volume", MountPath: "/bin/entrypoint.sh", SubPath: "entrypoint.sh", ReadOnly: true},
							{Name: "whatap-config-volume", MountPath: "/whatap_conf"},
						},
					},
				},
				InitContainers: []corev1.Container{
					{
						Name:    "whatap-node-debug",
						Image:   image,
						Command: []string{"/data/agent/tools/whatap_debugger", "run"},
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
						DefaultMode:          int32Ptr(0700),
					}}},
					{Name: "whatap-config-volume", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					{Name: "containerddomainsocket", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/run/containerd/containerd.sock"}}},
				},
			},
		},
	}
}

func createOrUpdateGpuConfigMap(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr *monitoringv2alpha1.WhatapAgent) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dcgm-exporter-csv",
			Namespace: r.DefaultNamespace,
		},
	}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		cm.Data = map[string]string{"whatap-gpu.csv": gpu.WhatapGPUMetricsCSV}
		return nil
	})
	if err != nil {
		logger.Error(err, "Fail create/update dcgm-exporter-csv ConfigMap")
		return err
	}
	logResult(logger, "Whatap", "dcgm-exporter-csv ConfigMap", op)
	return nil
}

// ---------- GPU Exporter 추가 함수 ----------

func addDcgmExporterToNodeAgent(podSpec *corev1.PodSpec) {
	dcgmContainer := corev1.Container{
		Name:  "dcgm-exporter",
		Image: "nvcr.io/nvidia/k8s/dcgm-exporter:4.2.0-4.1.0-ubuntu22.04",
		Env: []corev1.EnvVar{
			{Name: "DCGM_EXPORTER_LISTEN", Value: ":9400"},
			{Name: "DCGM_EXPORTER_KUBERNETES", Value: "true"},
			{Name: "DCGM_EXPORTER_COLLECTORS", Value: "/etc/dcgm-exporter/whatap-dcgm-exporter.csv"},
		},
		Ports: []corev1.ContainerPort{{Name: "metrics", ContainerPort: 9400}},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot: boolPtr(false),
			RunAsUser:    int64Ptr(0),
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{"SYS_ADMIN"},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "pod-gpu-resources", MountPath: "/var/lib/kubelet/pod-resources", ReadOnly: true},
			{Name: "whatap-dcgm-exporter-csv", MountPath: "/etc/dcgm-exporter/whatap-dcgm-exporter.csv", SubPath: "whatap-gpu.csv", ReadOnly: true},
		},
	}
	podSpec.Containers = append(podSpec.Containers, dcgmContainer)
	podSpec.Volumes = append(podSpec.Volumes,
		corev1.Volume{
			Name: "pod-gpu-resources",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/kubelet/pod-resources"},
			},
		},
		corev1.Volume{
			Name: "whatap-dcgm-exporter-csv",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "dcgm-exporter-csv"},
				},
			},
		},
	)
}

// 나머지 설치 함수(스펙이 없음)
func installApiserverMonitor(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr *monitoringv2alpha1.WhatapAgent) error {
	return nil
}
func installEtcdMonitor(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr *monitoringv2alpha1.WhatapAgent) error {
	return nil
}
func installSchedulerMonitor(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr *monitoringv2alpha1.WhatapAgent) error {
	return nil
}
func installOpenAgent(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr *monitoringv2alpha1.WhatapAgent) error {
	return nil
}
