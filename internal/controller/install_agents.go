package controller

import (
	"context"
	"fmt"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	"github.com/whatap/whatap-operator/internal/gpu"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
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
	var img string

	// Check if a full custom image name is provided
	if cr.Spec.Features.K8sAgent.CustomAgentImageFullName != "" {
		img = cr.Spec.Features.K8sAgent.CustomAgentImageFullName
	} else {
		// Use the separate name and version fields
		imgName := cr.Spec.Features.K8sAgent.AgentImageName
		if imgName == "" {
			imgName = "public.ecr.aws/whatap/kube_agent"
		}

		ver := cr.Spec.Features.K8sAgent.AgentImageVersion
		if ver == "" {
			ver = "latest"
		}
		img = fmt.Sprintf("%s:%s", imgName, ver)
	}

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

	// Get master agent container image
	masterImage := image
	if masterSpec.MasterAgentContainer != nil && masterSpec.MasterAgentContainer.Image != "" {
		masterImage = masterSpec.MasterAgentContainer.Image
	}

	// Get master agent container resources
	masterResources := *res
	if masterSpec.MasterAgentContainer != nil && masterSpec.MasterAgentContainer.Resources.Limits != nil {
		masterResources = masterSpec.MasterAgentContainer.Resources
	}

	// Get master agent container environment variables
	masterEnvs := []corev1.EnvVar{
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
	}

	// Add container-specific environment variables if provided
	if masterSpec.MasterAgentContainer != nil && len(masterSpec.MasterAgentContainer.Envs) > 0 {
		masterEnvs = append(masterEnvs, masterSpec.MasterAgentContainer.Envs...)
	} else if len(masterSpec.Envs) > 0 {
		// For backward compatibility, use the masterSpec.Envs if MasterAgentContainer.Envs is not provided
		masterEnvs = append(masterEnvs, masterSpec.Envs...)
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
						Image:   masterImage,
						Command: []string{"/bin/entrypoint.sh"},
						Ports:   []corev1.ContainerPort{{ContainerPort: 6600}},
						Env:     masterEnvs,
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
						Resources: masterResources,
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
	var img string

	// Check if a full custom image name is provided
	if cr.Spec.Features.K8sAgent.CustomAgentImageFullName != "" {
		img = cr.Spec.Features.K8sAgent.CustomAgentImageFullName
	} else {
		// Use the separate name and version fields
		imgName := cr.Spec.Features.K8sAgent.AgentImageName
		if imgName == "" {
			imgName = "public.ecr.aws/whatap/kube_agent"
		}

		ver := cr.Spec.Features.K8sAgent.AgentImageVersion
		if ver == "" {
			ver = "latest"
		}
		img = fmt.Sprintf("%s:%s", imgName, ver)
	}

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
			addDcgmExporterToNodeAgent(podSpec, cr)
		}
		return nil
	})
	if err != nil {
		logger.Error(err, "Fail create/update Whatap Node Agent DaemonSet")
		return err
	}
	logResult(logger, "Whatap", "Node Agent DaemonSet", op)

	// Create dcgm-exporter service if GPU monitoring is enabled and service is configured
	if cr.Spec.Features.K8sAgent.GpuMonitoring.Enabled {
		if err := ensureDcgmExporterService(ctx, r, logger, cr); err != nil {
			logger.Error(err, "Failed to create/update dcgm-exporter service")
			return err
		}
	}
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

	// Get node helper container resources
	helperResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resourceMustParse("100Mi"),
			corev1.ResourceCPU:    resourceMustParse("100m"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resourceMustParse("350Mi"),
			corev1.ResourceCPU:    resourceMustParse("200m"),
		},
	}
	if nodeSpec.NodeHelperContainer != nil && nodeSpec.NodeHelperContainer.Resources.Limits != nil {
		helperResources = nodeSpec.NodeHelperContainer.Resources
	}

	// Get node helper container image
	helperImage := image
	if nodeSpec.NodeHelperContainer != nil && nodeSpec.NodeHelperContainer.Image != "" {
		helperImage = nodeSpec.NodeHelperContainer.Image
	}

	// Get node helper container environment variables
	helperEnvs := []corev1.EnvVar{
		{
			Name: "NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
			},
		},
	}
	if nodeSpec.NodeHelperContainer != nil && len(nodeSpec.NodeHelperContainer.Envs) > 0 {
		helperEnvs = append(helperEnvs, nodeSpec.NodeHelperContainer.Envs...)
	}

	// Get node agent container image
	agentImage := image
	if nodeSpec.NodeAgentContainer != nil && nodeSpec.NodeAgentContainer.Image != "" {
		agentImage = nodeSpec.NodeAgentContainer.Image
	}

	// Get node agent container resources
	agentResources := *res
	if nodeSpec.NodeAgentContainer != nil && nodeSpec.NodeAgentContainer.Resources.Limits != nil {
		agentResources = nodeSpec.NodeAgentContainer.Resources
	}

	// Get node agent container environment variables
	agentEnvs := []corev1.EnvVar{
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
		{Name: "whatap_server_agent_enabled", Value: "true"},
	}

	// Add container-specific environment variables if provided
	if nodeSpec.NodeAgentContainer != nil && len(nodeSpec.NodeAgentContainer.Envs) > 0 {
		agentEnvs = append(agentEnvs, nodeSpec.NodeAgentContainer.Envs...)
	} else if len(nodeSpec.Envs) > 0 {
		// For backward compatibility, use the nodeSpec.Envs if NodeAgentContainer.Envs is not provided
		agentEnvs = append(agentEnvs, nodeSpec.Envs...)
	}

	// Get runtime configuration (default to containerd)
	runtime := "containerd"
	if nodeSpec.Runtime != "" {
		runtime = nodeSpec.Runtime
	}

	// Define runtime socket configurations
	var runtimeVolumeMount corev1.VolumeMount
	var runtimeVolume corev1.Volume

	switch runtime {
	case "docker":
		runtimeVolumeMount = corev1.VolumeMount{
			Name:      "dockerdomainsocket",
			MountPath: "/var/run/docker.sock",
		}
		runtimeVolume = corev1.Volume{
			Name: "dockerdomainsocket",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{Path: "/var/run/docker.sock"},
			},
		}
	case "crio":
		runtimeVolumeMount = corev1.VolumeMount{
			Name:      "criodomainsocket",
			MountPath: "/var/run/crio/crio.sock",
		}
		runtimeVolume = corev1.Volume{
			Name: "criodomainsocket",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{Path: "/var/run/crio/crio.sock"},
			},
		}
	default: // containerd
		runtimeVolumeMount = corev1.VolumeMount{
			Name:      "containerddomainsocket",
			MountPath: "/run/containerd/containerd.sock",
		}
		runtimeVolume = corev1.Volume{
			Name: "containerddomainsocket",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{Path: "/run/containerd/containerd.sock"},
			},
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
						Name:      "whatap-node-helper",
						Image:     helperImage,
						Command:   []string{"/data/agent/node/cadvisor_helper", "-port", "6801"},
						Ports:     []corev1.ContainerPort{{Name: "helperport", ContainerPort: 6801}},
						Env:       helperEnvs,
						Resources: helperResources,
						VolumeMounts: []corev1.VolumeMount{
							{Name: "rootfs", MountPath: "/rootfs", ReadOnly: true},
							{Name: "hostsys", MountPath: "/sys", ReadOnly: true},
							{Name: "hostdiskdevice", MountPath: "/dev/disk", ReadOnly: true},
							runtimeVolumeMount,
						},
					},
					{
						Name:      "whatap-node-agent",
						Image:     agentImage,
						Command:   []string{"/bin/entrypoint.sh"},
						Ports:     []corev1.ContainerPort{{Name: "nodeport", ContainerPort: 6600}},
						Env:       agentEnvs,
						Resources: agentResources,
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
					runtimeVolume,
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

func addDcgmExporterToNodeAgent(podSpec *corev1.PodSpec, cr *monitoringv2alpha1.WhatapAgent) {
	// Check if a custom image is specified
	dcgmImage := "public.ecr.aws/whatap/dcgm-exporter:4.3.1-4.4.0-ubuntu22.04"
	if cr.Spec.Features.K8sAgent.GpuMonitoring.CustomImageFullName != "" {
		dcgmImage = cr.Spec.Features.K8sAgent.GpuMonitoring.CustomImageFullName
	}

	dcgmContainer := corev1.Container{
		Name:  "dcgm-exporter",
		Image: dcgmImage,
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

// ensureDcgmExporterService creates or updates the service for dcgm-exporter
func ensureDcgmExporterService(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr *monitoringv2alpha1.WhatapAgent) error {
	// Check if service is enabled
	if cr.Spec.Features.K8sAgent.GpuMonitoring.Service == nil || !cr.Spec.Features.K8sAgent.GpuMonitoring.Service.Enabled {
		return nil
	}

	// Determine the namespace for the service
	serviceNamespace := r.DefaultNamespace
	if cr.Spec.Features.K8sAgent.Namespace != "" {
		serviceNamespace = cr.Spec.Features.K8sAgent.Namespace
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dcgm-exporter-service",
			Namespace: serviceNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "dcgm-exporter",
				"app.kubernetes.io/managed-by": "whatap-operator",
			},
		},
	}

	// Set WhatapAgent instance as the owner and controller
	logger.Info("Setting controller reference for resource",
		"resourceType", "Service",
		"resourceName", svc.Name,
		"resourceNamespace", svc.Namespace,
		"ownerType", "WhatapAgent",
		"ownerName", cr.Name,
		"ownerNamespace", cr.Namespace)
	if err := controllerutil.SetControllerReference(cr, svc, r.Scheme); err != nil {
		return err
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		// Set default values
		serviceType := corev1.ServiceTypeClusterIP
		port := int32(9400)

		// Use configured values if provided
		if cr.Spec.Features.K8sAgent.GpuMonitoring.Service.Type != "" {
			serviceType = cr.Spec.Features.K8sAgent.GpuMonitoring.Service.Type
		}
		if cr.Spec.Features.K8sAgent.GpuMonitoring.Service.Port != 0 {
			port = cr.Spec.Features.K8sAgent.GpuMonitoring.Service.Port
		}

		svc.Spec = corev1.ServiceSpec{
			Selector: map[string]string{
				"name": "whatap-node-agent",
			},
			Type: serviceType,
			Ports: []corev1.ServicePort{{
				Name:       "metrics",
				Port:       port,
				TargetPort: intstr.FromInt32(9400),
				Protocol:   corev1.ProtocolTCP,
			}},
		}

		// Set NodePort if specified and service type is NodePort
		if serviceType == corev1.ServiceTypeNodePort && cr.Spec.Features.K8sAgent.GpuMonitoring.Service.NodePort != 0 {
			svc.Spec.Ports[0].NodePort = cr.Spec.Features.K8sAgent.GpuMonitoring.Service.NodePort
		}

		return nil
	})

	if err != nil {
		logger.Error(err, "Failed to create/update dcgm-exporter service")
		return err
	}

	logger.Info("Successfully created/updated dcgm-exporter service")
	return nil
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
func generateScrapeConfig(cr *monitoringv2alpha1.WhatapAgent, defaultNamespace string) string {
	// Define the structure for the scrape config
	type ScrapeConfig struct {
		Features struct {
			OpenAgent struct {
				Enabled bool          `yaml:"enabled"`
				Targets []interface{} `yaml:"targets,omitempty"`
			} `yaml:"openAgent"`
		} `yaml:"features"`
	}

	// Create the scrape config
	config := ScrapeConfig{}
	config.Features.OpenAgent.Enabled = cr.Spec.Features.OpenAgent.Enabled

	// Convert targets to interface{} for YAML marshaling
	for _, target := range cr.Spec.Features.OpenAgent.Targets {
		// Skip disabled targets
		if !target.Enabled {
			continue
		}

		targetMap := make(map[string]interface{})
		targetMap["targetName"] = target.TargetName
		targetMap["type"] = target.Type
		targetMap["enabled"] = target.Enabled

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

				// Add port for PodMonitor/ServiceMonitor or address for StaticEndpoints
				if endpoint.Port != "" {
					endpointMap["port"] = endpoint.Port
				}
				if endpoint.Address != "" {
					endpointMap["address"] = endpoint.Address
				}

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

					// Add CA configuration (prefer file path over secret)
					if endpoint.TLSConfig.CAFile != "" {
						tlsConfig["caFile"] = endpoint.TLSConfig.CAFile
					} else if endpoint.TLSConfig.CASecret != nil {
						tlsConfig["caFile"] = fmt.Sprintf("/etc/ssl/certs/%s/%s",
							endpoint.TLSConfig.CASecret.Name,
							endpoint.TLSConfig.CASecret.Key)
					}

					// Add client certificate configuration (prefer file path over secret)
					if endpoint.TLSConfig.CertFile != "" {
						tlsConfig["certFile"] = endpoint.TLSConfig.CertFile
					} else if endpoint.TLSConfig.CertSecret != nil {
						tlsConfig["certFile"] = fmt.Sprintf("/etc/ssl/certs/%s/%s",
							endpoint.TLSConfig.CertSecret.Name,
							endpoint.TLSConfig.CertSecret.Key)
					}

					// Add client key configuration (prefer file path over secret)
					if endpoint.TLSConfig.KeyFile != "" {
						tlsConfig["keyFile"] = endpoint.TLSConfig.KeyFile
					} else if endpoint.TLSConfig.KeySecret != nil {
						tlsConfig["keyFile"] = fmt.Sprintf("/etc/ssl/certs/%s/%s",
							endpoint.TLSConfig.KeySecret.Name,
							endpoint.TLSConfig.KeySecret.Key)
					}

					// Add server name if specified
					if endpoint.TLSConfig.ServerName != "" {
						tlsConfig["serverName"] = endpoint.TLSConfig.ServerName
					}

					endpointMap["tlsConfig"] = tlsConfig
				}

				// Add params if present
				if len(endpoint.Params) > 0 {
					endpointMap["params"] = endpoint.Params
				}

				// Add addNodeLabel if present
				if endpoint.AddNodeLabel {
					endpointMap["addNodeLabel"] = endpoint.AddNodeLabel
				}

				// Add metricRelabelConfigs if present at endpoint level
				if len(endpoint.MetricRelabelConfigs) > 0 {
					relabelConfigs := make([]interface{}, 0)
					for _, relabelConfig := range endpoint.MetricRelabelConfigs {
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
					endpointMap["metricRelabelConfigs"] = relabelConfigs
				}

				endpoints = append(endpoints, endpointMap)
			}
			targetMap["endpoints"] = endpoints
		}

		config.Features.OpenAgent.Targets = append(config.Features.OpenAgent.Targets, targetMap)
	}

	// Auto-add GPU monitoring target if gpuMonitoring is enabled
	if cr.Spec.Features.K8sAgent.GpuMonitoring.Enabled {
		// Check for duplicate targets to avoid conflicts
		gpuTargetName := "dcgm-exporter-auto"
		isDuplicate := false

		for _, existingTarget := range config.Features.OpenAgent.Targets {
			if targetMap, ok := existingTarget.(map[string]interface{}); ok {
				if targetName, exists := targetMap["targetName"]; exists && targetName == gpuTargetName {
					isDuplicate = true
					break
				}
				// Also check if there's already a target for whatap-node-agent pods
				if selector, exists := targetMap["selector"]; exists {
					if selectorMap, ok := selector.(map[string]interface{}); ok {
						if matchLabels, exists := selectorMap["matchLabels"]; exists {
							if labelsMap, ok := matchLabels.(map[string]string); ok {
								if labelsMap["name"] == "whatap-node-agent" {
									isDuplicate = true
									break
								}
							}
						}
					}
				}
			}
		}

		// Only add GPU target if no duplicate exists
		if !isDuplicate {
			gpuTargetMap := make(map[string]interface{})
			gpuTargetMap["targetName"] = gpuTargetName
			gpuTargetMap["type"] = "PodMonitor"
			gpuTargetMap["enabled"] = true

			// Use dynamic namespace with proper priority
			targetNamespace := defaultNamespace // Use the passed default namespace
			if cr.Spec.Features.K8sAgent.Namespace != "" {
				// Override with CR-specified namespace if provided
				targetNamespace = cr.Spec.Features.K8sAgent.Namespace
			}

			// Set namespace selector to target the appropriate namespace
			nsSelector := make(map[string]interface{})
			nsSelector["matchNames"] = []string{targetNamespace}
			gpuTargetMap["namespaceSelector"] = nsSelector

			// Set pod selector to target whatap-node-agent pods
			selector := make(map[string]interface{})
			selector["matchLabels"] = map[string]string{
				"name": "whatap-node-agent",
			}
			gpuTargetMap["selector"] = selector

			// Set endpoint configuration for DCGM exporter with customizable options
			// Create two endpoints: one for regular metrics and one for process metrics
			endpoints := make([]interface{}, 2)

			// First endpoint: regular metrics
			endpointMap1 := make(map[string]interface{})
			endpointMap1["port"] = "9400"
			endpointMap1["path"] = "/metrics"

			// Allow customization of scraping interval
			interval := "30s" // Default interval
			endpointMap1["interval"] = interval

			endpointMap1["scheme"] = "http"

			// Add addNodeLabel at endpoint level
			endpointMap1["addNodeLabel"] = true

			// Add metricRelabelConfigs at endpoint level for GPU monitoring
			metricRelabelConfigs1 := make([]interface{}, 2)

			// First relabel config: add wtp_src label
			relabelConfig1 := make(map[string]interface{})
			relabelConfig1["target_label"] = "wtp_src"
			relabelConfig1["replacement"] = "true"
			relabelConfig1["action"] = "replace"
			metricRelabelConfigs1[0] = relabelConfig1

			// Second relabel config: keep only DCGM metrics
			relabelConfig2 := make(map[string]interface{})
			relabelConfig2["source_labels"] = []string{"__name__"}
			relabelConfig2["regex"] = "DCGM.*"
			relabelConfig2["action"] = "keep"
			metricRelabelConfigs1[1] = relabelConfig2

			endpointMap1["metricRelabelConfigs"] = metricRelabelConfigs1

			endpoints[0] = endpointMap1

			// Second endpoint: process metrics
			endpointMap2 := make(map[string]interface{})
			endpointMap2["port"] = "9400"
			endpointMap2["path"] = "/metrics/process"
			endpointMap2["interval"] = interval
			endpointMap2["scheme"] = "http"
			endpointMap2["addNodeLabel"] = true

			// Add the same metricRelabelConfigs for process metrics
			metricRelabelConfigs2 := make([]interface{}, 2)

			// First relabel config: add wtp_src label
			relabelConfig3 := make(map[string]interface{})
			relabelConfig3["target_label"] = "wtp_src"
			relabelConfig3["replacement"] = "true"
			relabelConfig3["action"] = "replace"
			metricRelabelConfigs2[0] = relabelConfig3

			// Second relabel config: keep only DCGM metrics
			relabelConfig4 := make(map[string]interface{})
			relabelConfig4["source_labels"] = []string{"__name__"}
			relabelConfig4["regex"] = "DCGM.*"
			relabelConfig4["action"] = "keep"
			metricRelabelConfigs2[1] = relabelConfig4

			endpointMap2["metricRelabelConfigs"] = metricRelabelConfigs2

			endpoints[1] = endpointMap2
			gpuTargetMap["endpoints"] = endpoints

			config.Features.OpenAgent.Targets = append(config.Features.OpenAgent.Targets, gpuTargetMap)
		}
	}

	// Marshal to YAML
	yamlBytes, err := yaml.Marshal(config)
	if err != nil {
		return "# Error generating scrape config: " + err.Error()
	}

	return string(yamlBytes)
}

// collectTLSSecrets collects all TLS Secrets from the targets
func collectTLSSecrets(targets []monitoringv2alpha1.OpenAgentTargetSpec) map[string][]string {
	secrets := make(map[string][]string)

	for _, target := range targets {
		for _, endpoint := range target.Endpoints {
			if endpoint.TLSConfig != nil {
				// CA Secret
				if endpoint.TLSConfig.CASecret != nil {
					secretName := endpoint.TLSConfig.CASecret.Name
					key := endpoint.TLSConfig.CASecret.Key
					if _, exists := secrets[secretName]; !exists {
						secrets[secretName] = []string{}
					}
					if !contains(secrets[secretName], key) {
						secrets[secretName] = append(secrets[secretName], key)
					}
				}

				// Cert Secret
				if endpoint.TLSConfig.CertSecret != nil {
					secretName := endpoint.TLSConfig.CertSecret.Name
					key := endpoint.TLSConfig.CertSecret.Key
					if _, exists := secrets[secretName]; !exists {
						secrets[secretName] = []string{}
					}
					if !contains(secrets[secretName], key) {
						secrets[secretName] = append(secrets[secretName], key)
					}
				}

				// Key Secret
				if endpoint.TLSConfig.KeySecret != nil {
					secretName := endpoint.TLSConfig.KeySecret.Name
					key := endpoint.TLSConfig.KeySecret.Key
					if _, exists := secrets[secretName]; !exists {
						secrets[secretName] = []string{}
					}
					if !contains(secrets[secretName], key) {
						secrets[secretName] = append(secrets[secretName], key)
					}
				}
			}
		}
	}

	return secrets
}

// contains checks if a string slice contains a specific string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
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
				Resources: []string{"pods", "services", "endpoints", "namespaces", "configmaps", "secrets"},
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
		scrapeConfig := generateScrapeConfig(cr, r.DefaultNamespace)
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

	// Create or update the Deployment with retry logic
	// This helps handle concurrent modification errors
	maxRetries := 3
	retryCount := 0
	var deployOp controllerutil.OperationResult

	for retryCount < maxRetries {
		// Get a fresh deployment object for each retry
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "whatap-open-agent",
				Namespace: r.DefaultNamespace,
			},
		}

		// Get the OpenAgent spec for easier access
		openAgentSpec := cr.Spec.Features.OpenAgent

		// Set up base labels
		deploy.Labels = map[string]string{
			"app": "whatap-open-agent",
		}

		// Apply custom labels if provided
		if openAgentSpec.Labels != nil {
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

		deployOp, err = controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
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

			// Prepare volumes and volume mounts
			volumes := []corev1.Volume{
				{
					Name: "logs-volume",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
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
			}

			volumeMounts := []corev1.VolumeMount{
				{
					Name:      "logs-volume",
					MountPath: "/app/logs",
				},
				{
					Name:      "config-volume",
					MountPath: "/app/scrape_config.yaml",
					SubPath:   "scrape_config.yaml",
				},
			}

			// Add TLS Secret volumes and volume mounts
			tlsSecrets := collectTLSSecrets(cr.Spec.Features.OpenAgent.Targets)
			for secretName, secretKeys := range tlsSecrets {
				volumeName := fmt.Sprintf("tls-secret-%s", secretName)

				// Add Secret volume
				volumes = append(volumes, corev1.Volume{
					Name: volumeName,
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: secretName,
							Items: func() []corev1.KeyToPath {
								var items []corev1.KeyToPath
								for _, key := range secretKeys {
									items = append(items, corev1.KeyToPath{
										Key:  key,
										Path: key,
									})
								}
								return items
							}(),
						},
					},
				})

				// Add volume mount
				volumeMounts = append(volumeMounts, corev1.VolumeMount{
					Name:      volumeName,
					MountPath: fmt.Sprintf("/etc/ssl/certs/%s", secretName),
					ReadOnly:  true,
				})
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
								Image:           getOpenAgentImage(openAgentSpec),
								ImagePullPolicy: corev1.PullAlways,
								Command:         getOpenAgentCommand(openAgentSpec),
								Args:            getOpenAgentArgs(openAgentSpec),
								Env: append([]corev1.EnvVar{
									getWhatapLicenseEnvVar(cr),
									getWhatapHostEnvVar(cr),
									getWhatapPortEnvVar(cr),
								}, openAgentSpec.Envs...),
								VolumeMounts: volumeMounts,
							},
						},
						Volumes: volumes,
					},
				},
			}
			return nil
		})

		if err == nil {
			// Success, break out of the retry loop
			logResult(logger, "Whatap", "OpenAgent Deployment", deployOp)
			break
		}

		// Check if we should retry
		if retryCount < maxRetries-1 {
			retryCount++
			logger.Info("Retrying deployment update due to conflict", "attempt", retryCount, "maxRetries", maxRetries)
			// Simple exponential backoff
			time.Sleep(time.Duration(retryCount*100) * time.Millisecond)
		} else {
			// Max retries reached, return the error
			logger.Error(err, "Failed to create/update Deployment for OpenAgent after retries")
			return err
		}
	}

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
				Key: "WHATAP_LICENSE",
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
				Key: "WHATAP_HOST",
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
				Key: "WHATAP_PORT",
			},
		},
	}
}

// getOpenAgentCommand returns the command for the OpenAgent container
// Returns nil to use the default command from the image
func getOpenAgentCommand(spec monitoringv2alpha1.OpenAgentSpec) []string {
	// Use default command from image
	return nil
}

// getOpenAgentArgs returns the args for the OpenAgent container
// If DisableForeground is true, adds the daemon flag to run in background mode
func getOpenAgentArgs(spec monitoringv2alpha1.OpenAgentSpec) []string {
	if spec.DisableForeground {
		return []string{"-d"}
	}
	// Use default args from image (foreground mode)
	return nil
}

// getOpenAgentImage returns the image string for the OpenAgent
// If a full custom image name is provided in the CR, it will use that
// Otherwise, if custom image name or version is provided, it will use those values
// Otherwise, it falls back to the default values
func getOpenAgentImage(spec monitoringv2alpha1.OpenAgentSpec) string {
	// Check if a full custom image name is provided
	if spec.CustomImageFullName != "" {
		return spec.CustomImageFullName
	}

	// Otherwise, use the separate name and version fields
	imageName := "public.ecr.aws/whatap/open_agent"
	imageVersion := "latest"

	if spec.ImageName != "" {
		imageName = spec.ImageName
	}

	if spec.ImageVersion != "" {
		imageVersion = spec.ImageVersion
	}

	return fmt.Sprintf("%s:%s", imageName, imageVersion)
}
