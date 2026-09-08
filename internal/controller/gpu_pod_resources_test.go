package controller

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGpuPodResourcesPath(t *testing.T) {
	tests := []struct {
		name     string
		gpuJSON  string
		wantPath string
	}{
		{name: "omitted uses existing default", gpuJSON: `{"enabled":true}`, wantPath: "/var/lib/kubelet/pod-resources"},
		{name: "empty uses existing default", gpuJSON: `{"enabled":true,"podResourcesPath":""}`, wantPath: "/var/lib/kubelet/pod-resources"},
		{name: "custom host directory", gpuJSON: `{"enabled":true,"podResourcesPath":"/repo.p/kubelet/pod-resources"}`, wantPath: "/repo.p/kubelet/pod-resources"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := &monitoringv2alpha1.WhatapAgent{}
			if err := json.Unmarshal([]byte(tt.gpuJSON), &cr.Spec.Features.K8sAgent.GpuMonitoring); err != nil {
				t.Fatal(err)
			}
			podSpec := corev1.PodSpec{}
			addDcgmExporterToNodeAgent(&podSpec, cr)
			assertGpuPodResources(t, &podSpec, tt.wantPath)
		})
	}
}

func TestGpuPodResourcesPathReconciliation(t *testing.T) {
	for _, mode := range []string{"shared daemonset", "node selector", "node affinity", "gpu disabled"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, appsv1.AddToScheme, monitoringv2alpha1.AddToScheme} {
				if err := add(scheme); err != nil {
					t.Fatal(err)
				}
			}
			cr := &monitoringv2alpha1.WhatapAgent{ObjectMeta: metav1.ObjectMeta{Name: "whatap", UID: types.UID("test-whatap")}}
			gpu := &cr.Spec.Features.K8sAgent.GpuMonitoring
			gpu.Enabled = mode != "gpu disabled"
			gpu.Service = &monitoringv2alpha1.GpuMonitoringServiceSpec{Enabled: false}
			cr.Spec.Features.K8sAgent.NodeAgent.Runtime = "containerd"
			cr.Spec.Features.K8sAgent.NodeAgent.RuntimeSocketPath = "/custom/containerd/containerd.sock"
			gpuDaemonSet := "whatap-node-agent"
			switch mode {
			case "node selector":
				gpu.NodeSelector = map[string]string{"accelerator": "nvidia"}
				gpuDaemonSet = "whatap-node-agent-gpu"
			case "node affinity":
				gpu.Affinity = &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{
						MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "accelerator", Operator: corev1.NodeSelectorOpIn, Values: []string{"nvidia"}}},
					}}},
				}}
				gpuDaemonSet = "whatap-node-agent-gpu"
			}
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr).Build()
			reconciler := &WhatapAgentReconciler{Client: client, Scheme: scheme, DefaultNamespace: "whatap-monitoring"}
			for _, path := range []string{"", "/repo.p/kubelet/pod-resources", "/other/kubelet/pod-resources", ""} {
				gpu.PodResourcesPath = path
				wantPath := path
				if wantPath == "" {
					wantPath = "/var/lib/kubelet/pod-resources"
				}
				// DeepCopy guards API field propagation, including a fresh controller reading the CR.
				if err := createOrUpdateNodeAgent(ctx, reconciler, logr.Discard(), cr.DeepCopy()); err != nil {
					t.Fatal(err)
				}
				ds := &appsv1.DaemonSet{}
				key := types.NamespacedName{Namespace: "whatap-monitoring", Name: gpuDaemonSet}
				if err := client.Get(ctx, key, ds); err != nil {
					t.Fatal(err)
				}
				if gpu.Enabled {
					assertGpuPodResources(t, &ds.Spec.Template.Spec, wantPath)
				}
				if !gpu.Enabled || gpuDaemonSet != "whatap-node-agent" {
					normal := &appsv1.DaemonSet{}
					if err := client.Get(ctx, types.NamespacedName{Namespace: "whatap-monitoring", Name: "whatap-node-agent"}, normal); err != nil {
						t.Fatal(err)
					}
					for _, volume := range normal.Spec.Template.Spec.Volumes {
						if volume.Name == "pod-gpu-resources" {
							t.Fatal("non-GPU daemonset must not mount pod-resources")
						}
					}
					for _, container := range normal.Spec.Template.Spec.Containers {
						if container.Name == "dcgm-exporter" {
							t.Fatal("non-GPU daemonset must not run dcgm-exporter")
						}
					}
				}
				runtimeSocketFound := false
				for _, volume := range ds.Spec.Template.Spec.Volumes {
					if volume.HostPath != nil && volume.HostPath.Path == "/custom/containerd/containerd.sock" {
						runtimeSocketFound = true
					}
				}
				if !runtimeSocketFound {
					t.Fatal("runtimeSocketPath must remain independent of podResourcesPath")
				}
				// Emulate an existing object and manual drift, then reconcile with a new controller.
				ds.CreationTimestamp = metav1.Now()
				for i := range ds.Spec.Template.Spec.Volumes {
					volume := &ds.Spec.Template.Spec.Volumes[i]
					if volume.Name == "pod-gpu-resources" {
						volume.HostPath.Path = "/manual-drift"
					}
				}
				if err := client.Update(ctx, ds); err != nil {
					t.Fatal(err)
				}
				reconciler = &WhatapAgentReconciler{Client: client, Scheme: scheme, DefaultNamespace: "whatap-monitoring"}
				if err := createOrUpdateNodeAgent(ctx, reconciler, logr.Discard(), cr.DeepCopy()); err != nil {
					t.Fatal(err)
				}
				if err := client.Get(ctx, key, ds); err != nil {
					t.Fatal(err)
				}
				if gpu.Enabled {
					assertGpuPodResources(t, &ds.Spec.Template.Spec, wantPath)
				}
			}
		})
	}
}

func assertGpuPodResources(t *testing.T, spec *corev1.PodSpec, wantHostPath string) {
	t.Helper()
	volumes, mounts := 0, 0
	for _, volume := range spec.Volumes {
		if volume.Name != "pod-gpu-resources" {
			continue
		}
		volumes++
		if volume.HostPath == nil || volume.HostPath.Path != wantHostPath {
			t.Errorf("pod-gpu-resources hostPath = %#v, want %q", volume.HostPath, wantHostPath)
		}
	}
	for _, container := range spec.Containers {
		if container.Name != "dcgm-exporter" {
			continue
		}
		for _, mount := range container.VolumeMounts {
			if mount.Name != "pod-gpu-resources" {
				continue
			}
			mounts++
			if mount.MountPath != "/var/lib/kubelet/pod-resources" || !mount.ReadOnly || mount.SubPath != "" {
				t.Errorf("exporter must retain its fixed read-only directory mount, got %#v", mount)
			}
		}
	}
	if volumes != 1 || mounts != 1 {
		t.Fatalf("pod-resources volumes=%d mounts=%d, want exactly one each", volumes, mounts)
	}
}
