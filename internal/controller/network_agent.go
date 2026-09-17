package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const networkAgentName = "whatap-network-agent"

func (r *WhatapAgentReconciler) reconcileNetworkAgent(ctx context.Context, cr *monitoringv2alpha1.WhatapAgent) error {
	cfg := cr.Spec.Features.NetworkAgent
	if !cfg.Enabled {
		return r.cleanupNetworkAgent(ctx, cr)
	}
	if strings.TrimSpace(cfg.Image) == "" {
		return fmt.Errorf("networkAgent.image is required when enabled")
	}
	if err := validateNetworkAgentEnv(cfg); err != nil {
		return err
	}
	loggingArgs, err := networkAgentLoggingArgs(cfg)
	if err != nil {
		return err
	}
	if cr.UID == "" {
		return fmt.Errorf("networkAgent requires a persisted CR UID")
	}
	nodeSelector := map[string]string{"kubernetes.io/os": "linux"}
	for key, value := range cfg.NodeSelector {
		if key == "kubernetes.io/os" && value != "linux" {
			return fmt.Errorf("networkAgent requires Linux nodes")
		}
		nodeSelector[key] = value
	}
	resources := cfg.Resources.DeepCopy()
	setDefaultResource(resources, corev1.ResourceList{corev1.ResourceCPU: resourceMustParse("50m"), corev1.ResourceMemory: resourceMustParse("128Mi")}, corev1.ResourceList{corev1.ResourceCPU: resourceMustParse("500m"), corev1.ResourceMemory: resourceMustParse("512Mi")})
	for name, limit := range resources.Limits {
		if limit.Sign() <= 0 {
			return fmt.Errorf("networkAgent.resources.limits[%s] must be positive", name)
		}
	}
	for name, request := range resources.Requests {
		if request.Sign() < 0 {
			return fmt.Errorf("networkAgent.resources.requests[%s] must not be negative", name)
		}
		if limit, ok := resources.Limits[name]; ok && request.Cmp(limit) > 0 {
			return fmt.Errorf("networkAgent.resources.requests[%s] exceeds its limit", name)
		}
	}
	// Leave headroom for BPF/native allocations outside Go's soft memory limit.
	goMemoryBytes := resources.Limits.Memory().Value() / 4 * 3
	if goMemoryBytes <= 0 {
		return fmt.Errorf("networkAgent memory limit is too small")
	}
	goMemoryLimit := fmt.Sprintf("%d", goMemoryBytes)
	if goMemoryBytes%(1<<20) == 0 {
		goMemoryLimit = fmt.Sprintf("%dMiB", goMemoryBytes/(1<<20))
	}
	ds := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: networkAgentName, Namespace: r.DefaultNamespace}}
	// Never adopt even an unowned pre-existing DaemonSet. Check before RBAC writes.
	if err := r.Get(ctx, client.ObjectKeyFromObject(ds), ds); err == nil {
		if !networkAgentOwned(ds, cr) {
			return fmt.Errorf("network agent collision: %s/%s is not owned by this CR", ds.Namespace, ds.Name)
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	if cfg.ServiceAccountName == "" {
		if err := r.ensureNetworkAgentRBAC(ctx, cr); err != nil {
			return err
		}
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, ds, func() error {
		if ds.ResourceVersion != "" && !networkAgentOwned(ds, cr) {
			return fmt.Errorf("network agent ownership collision")
		}
		if err := controllerutil.SetControllerReference(cr, ds, r.Scheme); err != nil {
			return err
		}
		secret := cfg.CredentialsSecretName
		if secret == "" {
			secret = "whatap-credentials"
		}
		sa := cfg.ServiceAccountName
		if sa == "" {
			sa = networkAgentName
		}
		env := []corev1.EnvVar{
			{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "spec.nodeName"}}},
			{Name: "WHATAP_OBJECT_NAME", Value: "network-$(NODE_NAME)"},
		}
		for _, pair := range [][2]string{{"WHATAP_ACCESSKEY", "WHATAP_LICENSE"}, {"WHATAP_SERVER_HOST", "WHATAP_HOST"}, {"WHATAP_SERVER_PORT", "WHATAP_PORT"}} {
			env = append(env, corev1.EnvVar{Name: pair[0], ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret}, Key: pair[1]}}})
		}
		env = append(env, corev1.EnvVar{Name: "GOMAXPROCS", Value: "1"}, corev1.EnvVar{Name: "GOMEMLIMIT", Value: goMemoryLimit})
		// Preserve expansion/duplicate order without aliasing CR-owned pointers.
		for _, entry := range cfg.Env {
			copied := entry.DeepCopy()
			if source := copied.ValueFrom; source != nil {
				if source.FieldRef != nil && source.FieldRef.APIVersion == "" {
					source.FieldRef.APIVersion = "v1"
				}
				if source.ResourceFieldRef != nil && source.ResourceFieldRef.Divisor.IsZero() {
					source.ResourceFieldRef.Divisor = resourceMustParse("1")
				}
			}
			env = append(env, *copied)
		}
		var envFrom []corev1.EnvFromSource
		for _, source := range cfg.EnvFrom {
			envFrom = append(envFrom, *source.DeepCopy())
		}
		var volumes []corev1.Volume
		var mounts []corev1.VolumeMount
		directory := corev1.HostPathDirectory
		for _, name := range []string{"tracing", "debug", "btf"} {
			path := "/sys/kernel/" + name
			volumes = append(volumes, corev1.Volume{Name: name, VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: path, Type: &directory}}})
			mounts = append(mounts, corev1.VolumeMount{Name: name, MountPath: path, ReadOnly: true})
		}
		args := []string{"-source=ebpf", "-output-mode=windows", "-export=tagcount", "-window=5s", "-pod-identity=true"}
		args = append(args, loggingArgs...)
		container := corev1.Container{Name: networkAgentName, Image: cfg.Image, Args: args, Env: env, EnvFrom: envFrom, Resources: *resources, VolumeMounts: mounts, SecurityContext: &corev1.SecurityContext{RunAsUser: int64Ptr(0), RunAsNonRoot: boolPtr(false), Privileged: boolPtr(true), ReadOnlyRootFilesystem: boolPtr(true)}}
		applyContainerDefaults(&container)
		labels := map[string]string{"app.kubernetes.io/name": networkAgentName}
		unavailable := intstr.FromInt32(1)
		var surge *intstr.IntOrString
		// Older servers prune MaxSurge; preserve nil unless the stored object supports it.
		if rolling := ds.Spec.UpdateStrategy.RollingUpdate; rolling != nil && rolling.MaxSurge != nil {
			zero := intstr.FromInt32(0)
			surge = &zero
		}
		ds.Spec = appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels}, RevisionHistoryLimit: int32Ptr(10),
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{Type: appsv1.RollingUpdateDaemonSetStrategyType, RollingUpdate: &appsv1.RollingUpdateDaemonSet{MaxUnavailable: &unavailable, MaxSurge: surge}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					HostPID: true, HostNetwork: true, DNSPolicy: corev1.DNSClusterFirstWithHostNet,
					ServiceAccountName: sa, AutomountServiceAccountToken: boolPtr(true),
					RestartPolicy: corev1.RestartPolicyAlways, TerminationGracePeriodSeconds: int64Ptr(30),
					SchedulerName: corev1.DefaultSchedulerName, SecurityContext: &corev1.PodSecurityContext{},
					Containers: []corev1.Container{container}, Volumes: volumes,
					NodeSelector: nodeSelector, Tolerations: cfg.Tolerations, ImagePullSecrets: cfg.ImagePullSecrets,
				},
			},
		}
		return nil
	})
	return err
}

// Only forward explicitly configured flags. Older images remain usable when
// logging fields are absent; new images supply their quiet export defaults.
func networkAgentLoggingArgs(cfg monitoringv2alpha1.NetworkAgentSpec) ([]string, error) {
	var args []string
	switch cfg.Stdout {
	case "":
	case "auto", "jsonl", "none":
		args = append(args, "-stdout="+cfg.Stdout)
	default:
		return nil, fmt.Errorf("networkAgent.stdout must be auto, jsonl or none")
	}
	switch cfg.LogLevel {
	case "":
	case "debug", "info", "warn", "error":
		args = append(args, "-log-level="+cfg.LogLevel)
	default:
		return nil, fmt.Errorf("networkAgent.logLevel must be debug, info, warn or error")
	}
	if cfg.LogInterval != "" {
		interval, err := time.ParseDuration(cfg.LogInterval)
		if err != nil || interval < 0 {
			return nil, fmt.Errorf("networkAgent.logInterval must be a nonnegative duration")
		}
		args = append(args, "-log-interval="+cfg.LogInterval)
	}
	return args, nil
}

// Validate shape only, never fetch sources or include user values in errors.
// Logging values are validated by the agent after Kubernetes resolves sources.
func validateNetworkAgentEnv(cfg monitoringv2alpha1.NetworkAgentSpec) error {
	invalid := func(path, reason string) error { return fmt.Errorf("networkAgent.%s %s", path, reason) }
	ref := func(path, name, key string) error {
		if len(validation.IsDNS1123Subdomain(name)) != 0 {
			return invalid(path+".name", "must be a valid resource name")
		}
		if len(validation.IsConfigMapKey(key)) != 0 {
			return invalid(path+".key", "must be a valid key")
		}
		return nil
	}
	for i, entry := range cfg.Env {
		path := fmt.Sprintf("env[%d]", i)
		switch entry.Name {
		case "NODE_NAME", "WHATAP_OBJECT_NAME", "WHATAP_ACCESSKEY", "WHATAP_SERVER_HOST", "WHATAP_SERVER_PORT", "GOMAXPROCS", "GOMEMLIMIT":
			return invalid(path+".name", "is reserved for the operator")
		}
		if len(validation.IsEnvVarName(entry.Name)) != 0 {
			return invalid(path+".name", "must be a valid environment name")
		}
		if source := entry.ValueFrom; source != nil {
			path += ".valueFrom"
			count := 0
			if source.SecretKeyRef != nil {
				count++
			}
			if source.ConfigMapKeyRef != nil {
				count++
			}
			if source.FieldRef != nil {
				count++
			}
			if source.ResourceFieldRef != nil {
				count++
			}
			if entry.Value != "" || count != 1 {
				return invalid(path, "requires exactly one source and no nonempty value")
			}
			if s := source.SecretKeyRef; s != nil {
				if err := ref(path+".secretKeyRef", s.Name, s.Key); err != nil {
					return err
				}
			}
			if s := source.ConfigMapKeyRef; s != nil {
				if err := ref(path+".configMapKeyRef", s.Name, s.Key); err != nil {
					return err
				}
			}
			if s := source.FieldRef; s != nil {
				if s.FieldPath == "" {
					return invalid(path+".fieldRef.fieldPath", "must not be empty")
				}
				if s.APIVersion != "" && s.APIVersion != "v1" {
					return invalid(path+".fieldRef.apiVersion", "must be v1")
				}
			}
			if s := source.ResourceFieldRef; s != nil {
				if s.Resource == "" {
					return invalid(path+".resourceFieldRef.resource", "must not be empty")
				}
				if s.Divisor.Sign() < 0 {
					return invalid(path+".resourceFieldRef.divisor", "must not be negative")
				}
			}
		}
	}
	for i, source := range cfg.EnvFrom {
		path := fmt.Sprintf("envFrom[%d]", i)
		if (source.ConfigMapRef == nil) == (source.SecretRef == nil) {
			return invalid(path, "requires exactly one source")
		}
		if source.Prefix != "" && len(validation.IsEnvVarName(source.Prefix)) != 0 {
			return invalid(path+".prefix", "must be a valid environment prefix")
		}
		if s := source.ConfigMapRef; s != nil && len(validation.IsDNS1123Subdomain(s.Name)) != 0 {
			return invalid(path+".configMapRef.name", "must be a valid resource name")
		}
		if s := source.SecretRef; s != nil && len(validation.IsDNS1123Subdomain(s.Name)) != 0 {
			return invalid(path+".secretRef.name", "must be a valid resource name")
		}
	}
	return nil
}

func networkAgentOwned(obj client.Object, cr *monitoringv2alpha1.WhatapAgent) bool {
	owner := metav1.GetControllerOf(obj)
	return cr.UID != "" && owner != nil && owner.UID == cr.UID && owner.Name == cr.Name && owner.Kind == "WhatapAgent" && owner.APIVersion == monitoringv2alpha1.GroupVersion.String()
}

func (r *WhatapAgentReconciler) networkAgentResources(cr *monitoringv2alpha1.WhatapAgent) []client.Object {
	// The CR is cluster-scoped. UID-qualified global names isolate separate CRs.
	name := networkAgentName + "-" + string(cr.UID)
	return []client.Object{
		&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: networkAgentName, Namespace: r.DefaultNamespace}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: networkAgentName, Namespace: r.DefaultNamespace}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: name}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name}},
	}
}

func (r *WhatapAgentReconciler) ensureNetworkAgentRBAC(ctx context.Context, cr *monitoringv2alpha1.WhatapAgent) error {
	objects := r.networkAgentResources(cr)
	for _, obj := range objects[1:] {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
			if obj.GetResourceVersion() != "" && !networkAgentOwned(obj, cr) {
				return fmt.Errorf("network agent ownership collision: %T %s", obj, obj.GetName())
			}
			if err := controllerutil.SetControllerReference(cr, obj, r.Scheme); err != nil {
				return err
			}
			switch v := obj.(type) {
			case *corev1.ServiceAccount:
				v.AutomountServiceAccountToken = boolPtr(true)
			case *rbacv1.ClusterRole:
				v.Rules = []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list", "watch"}}}
				v.AggregationRule = nil
			case *rbacv1.ClusterRoleBinding:
				v.RoleRef = rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: objects[2].GetName()}
				v.Subjects = []rbacv1.Subject{{Kind: "ServiceAccount", Name: networkAgentName, Namespace: r.DefaultNamespace}}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *WhatapAgentReconciler) cleanupNetworkAgent(ctx context.Context, cr *monitoringv2alpha1.WhatapAgent) error {
	for _, obj := range r.networkAgentResources(cr) {
		if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if !networkAgentOwned(obj, cr) {
			continue
		}
		uid, rv := obj.GetUID(), obj.GetResourceVersion()
		if err := r.Delete(ctx, obj, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &rv}}); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}
