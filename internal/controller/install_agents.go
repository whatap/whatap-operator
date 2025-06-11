package controller

import (
	"context"
	"fmt"
	"gopkg.in/yaml.v2"

	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	"github.com/whatap/whatap-operator/internal/gpu"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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

	// Get the master agent component spec for easier access
	masterSpec := cr.Spec.Features.K8sAgent.MasterAgent

	// Create deployment with base metadata
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "whatap-master-agent",
			Namespace: r.DefaultNamespace,
		},
	}

	// Apply custom labels if provided
	if masterSpec.Labels != nil {
		if deploy.Labels == nil {
			deploy.Labels = make(map[string]string)
		}
		for k, v := range masterSpec.Labels {
			deploy.Labels[k] = v
		}
	}

	// Apply custom annotations if provided
	if masterSpec.Annotations != nil {
		if deploy.Annotations == nil {
			deploy.Annotations = make(map[string]string)
		}
		for k, v := range masterSpec.Annotations {
			deploy.Annotations[k] = v
		}
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
	// Get the master agent component spec for easier access
	masterSpec := cr.Spec.Features.K8sAgent.MasterAgent

	// Create base labels and merge with custom labels if provided
	labels := map[string]string{"name": "whatap-master-agent"}
	if masterSpec.PodLabels != nil {
		for k, v := range masterSpec.PodLabels {
			labels[k] = v
		}
	}

	// Create pod annotations if provided
	var annotations map[string]string
	if masterSpec.PodAnnotations != nil {
		annotations = make(map[string]string)
		for k, v := range masterSpec.PodAnnotations {
			annotations[k] = v
		}
	}

	return appsv1.DeploymentSpec{
		Replicas: int32Ptr(1),
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"name": "whatap-master-agent"},
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: "whatap",
				// Apply tolerations from CR if specified
				Tolerations: masterSpec.Tolerations,
				Containers: []corev1.Container{
					{
						Name:    "whatap-master-agent",
						Image:   image,
						Command: []string{"/bin/entrypoint.sh"},
						Ports:   []corev1.ContainerPort{{ContainerPort: 6600}},
						Env: []corev1.EnvVar{
							getWhatapLicenseEnvVar(cr),
							getWhatapHostEnvVar(cr),
							getWhatapPortEnvVar(cr),
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

	// Get the node agent component spec for easier access
	nodeSpec := cr.Spec.Features.K8sAgent.NodeAgent

	// Create daemonset with base metadata
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "whatap-node-agent",
			Namespace: r.DefaultNamespace,
		},
	}

	// Apply custom labels if provided
	if nodeSpec.Labels != nil {
		if ds.Labels == nil {
			ds.Labels = make(map[string]string)
		}
		for k, v := range nodeSpec.Labels {
			ds.Labels[k] = v
		}
	}

	// Apply custom annotations if provided
	if nodeSpec.Annotations != nil {
		if ds.Annotations == nil {
			ds.Annotations = make(map[string]string)
		}
		for k, v := range nodeSpec.Annotations {
			ds.Annotations[k] = v
		}
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
	// Get the node agent component spec for easier access
	nodeSpec := cr.Spec.Features.K8sAgent.NodeAgent

	// Default tolerations for node agent
	defaultTolerations := []corev1.Toleration{
		{Key: "node-role.kubernetes.io/master", Effect: corev1.TaintEffectNoSchedule},
		{Key: "node-role.kubernetes.io/control-plane", Effect: corev1.TaintEffectNoSchedule},
	}

	// Merge default tolerations with any specified in the CR
	tolerations := append(defaultTolerations, nodeSpec.Tolerations...)

	// Create base labels and merge with custom labels if provided
	labels := map[string]string{"name": "whatap-node-agent"}
	if nodeSpec.PodLabels != nil {
		for k, v := range nodeSpec.PodLabels {
			labels[k] = v
		}
	}

	// Create pod annotations if provided
	var annotations map[string]string
	if nodeSpec.PodAnnotations != nil {
		annotations = make(map[string]string)
		for k, v := range nodeSpec.PodAnnotations {
			annotations[k] = v
		}
	}

	return appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"name": "whatap-node-agent"},
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      labels,
				Annotations: annotations,
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
							getWhatapLicenseEnvVar(cr),
							getWhatapHostEnvVar(cr),
							getWhatapPortEnvVar(cr),
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
				Tolerations: tolerations,
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
		Image: "nvcr.io/nvidia/k8s/dcgm-exporter:4.2.3-4.1.3-ubuntu22.04",
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

// generateScrapeConfig generates the scrape_config.yaml content from the CR
func generateScrapeConfig(cr *monitoringv2alpha1.WhatapAgent) string {
	// Define the structure for the scrape config
	type ScrapeConfig struct {
		Global struct {
			ScrapeInterval string `yaml:"scrape_interval"`
		} `yaml:"global"`
		Features struct {
			OpenAgent struct {
				Enabled        bool          `yaml:"enabled"`
				GlobalInterval string        `yaml:"globalInterval,omitempty"`
				GlobalPath     string        `yaml:"globalPath,omitempty"`
				Targets        []interface{} `yaml:"targets,omitempty"`
			} `yaml:"openAgent"`
		} `yaml:"features"`
	}

	// Create the scrape config
	config := ScrapeConfig{}
	config.Global.ScrapeInterval = "15s" // Default interval
	config.Features.OpenAgent.Enabled = cr.Spec.Features.OpenAgent.Enabled
	config.Features.OpenAgent.GlobalInterval = cr.Spec.Features.OpenAgent.GlobalInterval
	config.Features.OpenAgent.GlobalPath = cr.Spec.Features.OpenAgent.GlobalPath

	// Convert targets to interface{} for YAML marshaling
	for _, target := range cr.Spec.Features.OpenAgent.Targets {
		targetMap := make(map[string]interface{})
		targetMap["targetName"] = target.TargetName
		targetMap["type"] = target.Type

		// Add namespaceSelector if present
		if len(target.NamespaceSelector.MatchNames) > 0 || len(target.NamespaceSelector.MatchLabels) > 0 || len(target.NamespaceSelector.MatchExpressions) > 0 {
			nsSelector := make(map[string]interface{})
			if len(target.NamespaceSelector.MatchNames) > 0 {
				nsSelector["matchNames"] = target.NamespaceSelector.MatchNames
			}
			if len(target.NamespaceSelector.MatchLabels) > 0 {
				nsSelector["matchLabels"] = target.NamespaceSelector.MatchLabels
			}
			if len(target.NamespaceSelector.MatchExpressions) > 0 {
				// Convert match expressions to interface{}
				matchExpressions := make([]interface{}, 0)
				for _, expr := range target.NamespaceSelector.MatchExpressions {
					exprMap := make(map[string]interface{})
					exprMap["key"] = expr.Key
					exprMap["operator"] = expr.Operator
					exprMap["values"] = expr.Values
					matchExpressions = append(matchExpressions, exprMap)
				}
				nsSelector["matchExpressions"] = matchExpressions
			}
			targetMap["namespaceSelector"] = nsSelector
		}

		// Add selector if present
		if len(target.Selector.MatchLabels) > 0 || len(target.Selector.MatchExpressions) > 0 {
			selector := make(map[string]interface{})

			// Add matchLabels if present
			if len(target.Selector.MatchLabels) > 0 {
				selector["matchLabels"] = target.Selector.MatchLabels
			}

			// Add matchExpressions if present
			if len(target.Selector.MatchExpressions) > 0 {
				matchExpressions := make([]interface{}, 0)
				for _, expr := range target.Selector.MatchExpressions {
					exprMap := make(map[string]interface{})
					exprMap["key"] = expr.Key
					exprMap["operator"] = expr.Operator
					exprMap["values"] = expr.Values
					matchExpressions = append(matchExpressions, exprMap)
				}
				selector["matchExpressions"] = matchExpressions
			}

			targetMap["selector"] = selector
		}

		// Add endpoints if present
		if len(target.Endpoints) > 0 {
			endpoints := make([]interface{}, 0)
			for _, endpoint := range target.Endpoints {
				endpointMap := make(map[string]interface{})
				endpointMap["port"] = endpoint.Port
				if endpoint.Path != "" {
					endpointMap["path"] = endpoint.Path
				}
				if endpoint.Interval != "" {
					endpointMap["interval"] = endpoint.Interval
				}
				if endpoint.Scheme != "" {
					endpointMap["scheme"] = endpoint.Scheme
				}
				if endpoint.TLSConfig != nil {
					tlsConfig := make(map[string]interface{})
					tlsConfig["insecureSkipVerify"] = endpoint.TLSConfig.InsecureSkipVerify
					endpointMap["tlsConfig"] = tlsConfig
				}
				endpoints = append(endpoints, endpointMap)
			}
			targetMap["endpoints"] = endpoints
		}

		// Add metricRelabelConfigs if present
		if len(target.MetricRelabelConfigs) > 0 {
			relabelConfigs := make([]interface{}, 0)
			for _, relabelConfig := range target.MetricRelabelConfigs {
				relabelMap := make(map[string]interface{})
				if len(relabelConfig.SourceLabels) > 0 {
					relabelMap["source_labels"] = relabelConfig.SourceLabels
				}
				if relabelConfig.Regex != "" {
					relabelMap["regex"] = relabelConfig.Regex
				}
				if relabelConfig.TargetLabel != "" {
					relabelMap["target_label"] = relabelConfig.TargetLabel
				}
				if relabelConfig.Replacement != "" {
					relabelMap["replacement"] = relabelConfig.Replacement
				}
				if relabelConfig.Action != "" {
					relabelMap["action"] = relabelConfig.Action
				}
				relabelConfigs = append(relabelConfigs, relabelMap)
			}
			targetMap["metricRelabelConfigs"] = relabelConfigs
		}

		config.Features.OpenAgent.Targets = append(config.Features.OpenAgent.Targets, targetMap)
	}

	// Marshal to YAML
	yamlBytes, err := yaml.Marshal(config)
	if err != nil {
		return "# Error generating scrape config: " + err.Error()
	}

	return string(yamlBytes)
}

func installOpenAgent(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr *monitoringv2alpha1.WhatapAgent) error {
	// Create ServiceAccount
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "whatap-open-agent-sa",
			Namespace: r.DefaultNamespace,
		},
	}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error {
		return nil
	})
	if err != nil {
		logger.Error(err, "Failed to create/update ServiceAccount for OpenAgent")
		return err
	}
	logResult(logger, "Whatap", "OpenAgent ServiceAccount", op)

	// Create ClusterRole
	cr1 := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "whatap-open-agent-role",
		},
	}
	op, err = controllerutil.CreateOrUpdate(ctx, r.Client, cr1, func() error {
		cr1.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{"*"},
				Resources: []string{"pods", "services", "endpoints", "namespaces"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				NonResourceURLs: []string{"/metrics"},
				Verbs:           []string{"*"},
			},
		}
		return nil
	})
	if err != nil {
		logger.Error(err, "Failed to create/update ClusterRole for OpenAgent")
		return err
	}
	logResult(logger, "Whatap", "OpenAgent ClusterRole", op)

	// Create ClusterRoleBinding
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "whatap-open-agent-role-binding",
		},
	}
	op, err = controllerutil.CreateOrUpdate(ctx, r.Client, crb, func() error {
		crb.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "whatap-open-agent-sa",
				Namespace: r.DefaultNamespace,
			},
		}
		crb.RoleRef = rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     "whatap-open-agent-role",
			APIGroup: "rbac.authorization.k8s.io",
		}
		return nil
	})
	if err != nil {
		logger.Error(err, "Failed to create/update ClusterRoleBinding for OpenAgent")
		return err
	}
	logResult(logger, "Whatap", "OpenAgent ClusterRoleBinding", op)

	// Create ConfigMap
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "whatap-open-agent-config",
			Namespace: r.DefaultNamespace,
		},
	}
	op, err = controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		// Generate scrape_config.yaml content from CR
		scrapeConfig := generateScrapeConfig(cr)
		cm.Data = map[string]string{
			"scrape_config.yaml": scrapeConfig,
		}
		return nil
	})
	if err != nil {
		logger.Error(err, "Failed to create/update ConfigMap for OpenAgent")
		return err
	}
	logResult(logger, "Whatap", "OpenAgent ConfigMap", op)

	// Create Deployment
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "whatap-open-agent",
			Namespace: r.DefaultNamespace,
			Labels: map[string]string{
				"app": "whatap-open-agent",
			},
		},
	}

	// Get the OpenAgent spec for easier access
	openAgentSpec := cr.Spec.Features.OpenAgent

	// Apply custom labels if provided
	if openAgentSpec.Labels != nil {
		if deploy.Labels == nil {
			deploy.Labels = make(map[string]string)
		}
		for k, v := range openAgentSpec.Labels {
			deploy.Labels[k] = v
		}
	}

	// Apply custom annotations if provided
	if openAgentSpec.Annotations != nil {
		if deploy.Annotations == nil {
			deploy.Annotations = make(map[string]string)
		}
		for k, v := range openAgentSpec.Annotations {
			deploy.Annotations[k] = v
		}
	}

	op, err = controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		// Create base labels for pod template
		podLabels := map[string]string{"app": "whatap-open-agent"}
		if openAgentSpec.PodLabels != nil {
			for k, v := range openAgentSpec.PodLabels {
				podLabels[k] = v
			}
		}

		// Create pod annotations if provided
		var podAnnotations map[string]string
		if openAgentSpec.PodAnnotations != nil {
			podAnnotations = make(map[string]string)
			for k, v := range openAgentSpec.PodAnnotations {
				podAnnotations[k] = v
			}
		}

		deploy.Spec = appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "whatap-open-agent",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "whatap-open-agent-sa",
					// Apply tolerations from CR if specified
					Tolerations: openAgentSpec.Tolerations,
					Containers: []corev1.Container{
						{
							Name:            "whatap-open-agent",
							Image:           "whatap/open_agent:latest",
							ImagePullPolicy: corev1.PullAlways,
							Env: append([]corev1.EnvVar{
								{
									Name: "WHATAP_LICENSE",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "whatap-credentials",
											},
											Key: "license",
										},
									},
								},
								{
									Name: "WHATAP_HOST",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "whatap-credentials",
											},
											Key: "host",
										},
									},
								},
								{
									Name: "WHATAP_PORT",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "whatap-credentials",
											},
											Key: "port",
										},
									},
								},
							}, openAgentSpec.Envs...),
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config-volume",
									MountPath: "/app/scrape_config.yaml",
									SubPath:   "scrape_config.yaml",
								},
								{
									Name:      "logs-volume",
									MountPath: "/app/logs",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "whatap-open-agent-config",
									},
								},
							},
						},
						{
							Name: "logs-volume",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		}
		return nil
	})
	if err != nil {
		logger.Error(err, "Failed to create/update Deployment for OpenAgent")
		return err
	}
	logResult(logger, "Whatap", "OpenAgent Deployment", op)

	return nil
}

// Helper functions to get environment variables for Whatap credentials
// These functions check if the values are provided in the CR spec, and if not,
// they use the values from the whatap-credentials secret

func getWhatapLicenseEnvVar(cr *monitoringv2alpha1.WhatapAgent) corev1.EnvVar {
	if cr.Spec.License != "" {
		return corev1.EnvVar{Name: "WHATAP_LICENSE", Value: cr.Spec.License}
	}
	return corev1.EnvVar{
		Name: "WHATAP_LICENSE",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "whatap-credentials",
				},
				Key: "license",
			},
		},
	}
}

func getWhatapHostEnvVar(cr *monitoringv2alpha1.WhatapAgent) corev1.EnvVar {
	if cr.Spec.Host != "" {
		return corev1.EnvVar{Name: "WHATAP_HOST", Value: cr.Spec.Host}
	}
	return corev1.EnvVar{
		Name: "WHATAP_HOST",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "whatap-credentials",
				},
				Key: "host",
			},
		},
	}
}

func getWhatapPortEnvVar(cr *monitoringv2alpha1.WhatapAgent) corev1.EnvVar {
	if cr.Spec.Port != "" {
		return corev1.EnvVar{Name: "WHATAP_PORT", Value: cr.Spec.Port}
	}
	return corev1.EnvVar{
		Name: "WHATAP_PORT",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "whatap-credentials",
				},
				Key: "port",
			},
		},
	}
}
