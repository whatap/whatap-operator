package v2alpha1

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
)

type failingGetClient struct {
	client.Client
	err error
}

func (c failingGetClient) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return c.err
}

func TestDefaultDefersUnexpectedWhatapAgentReadFailureEventUntilPodPersistence(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaulter := &WhatapAgentCustomDefaulter{
		client: failingGetClient{
			Client: k8sClient,
			err:    errors.New("forbidden"),
		},
	}
	pod := &corev1.Pod{}
	pod.Namespace = "app"
	pod.Name = "example"

	if err := defaulter.Default(context.Background(), pod); err != nil {
		t.Fatalf("Default() error = %v", err)
	}
	if got := pod.Annotations["whatap-apm-injection-status"]; got != "failed" {
		t.Errorf("injection status = %q, want failed", got)
	}
	if got := pod.Annotations["whatap-apm-injection-reason"]; got != "WhatapAgentReadFailed" {
		t.Errorf("injection reason = %q, want WhatapAgentReadFailed", got)
	}
	assertNoEvent(t, k8sClient, pod.Namespace)
}

func TestDefaultMarksMissingWhatapAgentAsExpectedSkip(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := monitoringv2alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	defaulter := &WhatapAgentCustomDefaulter{
		client: fake.NewClientBuilder().WithScheme(scheme).Build(),
	}
	pod := &corev1.Pod{}
	pod.Namespace = "app"
	pod.Name = "example"

	if err := defaulter.Default(context.Background(), pod); err != nil {
		t.Fatalf("Default() error = %v", err)
	}
	if got := pod.Annotations["whatap-apm-injection-status"]; got != "skipped" {
		t.Errorf("injection status = %q, want skipped", got)
	}
	if got := pod.Annotations["whatap-apm-injection-reason"]; got != "WhatapAgentNotFound" {
		t.Errorf("injection reason = %q, want WhatapAgentNotFound", got)
	}
}

func TestDefaultDefersNamespaceReadFailureEventUntilPodPersistence(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := monitoringv2alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cr := &monitoringv2alpha1.WhatapAgent{}
	cr.Name = "whatap"
	cr.Spec.Features.Apm.Instrumentation.Enabled = true
	cr.Spec.Features.Apm.Instrumentation.Targets = []monitoringv2alpha1.TargetSpec{{
		Name:     "java-app",
		Enabled:  true,
		Language: "java",
		PodSelector: monitoringv2alpha1.PodSelector{
			MatchLabels: map[string]string{"app": "example"},
		},
	}}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr).Build()
	defaulter := &WhatapAgentCustomDefaulter{
		client: k8sClient,
	}
	pod := &corev1.Pod{}
	pod.Namespace = "missing-namespace"
	pod.Name = "example"
	pod.Labels = map[string]string{"app": "example"}

	if err := defaulter.Default(context.Background(), pod); err != nil {
		t.Fatalf("Default() error = %v", err)
	}
	if got := pod.Annotations["whatap-apm-injection-reason"]; got != "NamespaceReadFailed" {
		t.Errorf("injection reason = %q, want NamespaceReadFailed", got)
	}
	assertNoEvent(t, k8sClient, pod.Namespace)
}

func assertNoEvent(t *testing.T, k8sClient client.Client, namespace string) {
	t.Helper()
	events := &corev1.EventList{}
	if err := k8sClient.List(context.Background(), events, client.InNamespace(namespace)); err != nil {
		t.Fatal(err)
	}
	if got := len(events.Items); got != 0 {
		t.Fatalf("admission-time event count = %d, want 0", got)
	}
}

type recordingWhatapAgentClient struct {
	client.Client
	resource *monitoringv2alpha1.WhatapAgent
}

func (c *recordingWhatapAgentClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if err := c.Client.Get(ctx, key, obj, opts...); err != nil {
		return err
	}
	if cr, ok := obj.(*monitoringv2alpha1.WhatapAgent); ok {
		c.resource = cr
	}
	return nil
}

func TestDefaultSelectsPodConfigMapWithoutChangingCentralTarget(t *testing.T) {
	for _, tt := range []struct {
		name                    string
		annotation              string
		central                 string
		want                    string
		missingAnnotation       bool
		podMismatch             bool
		namespaceMismatch       bool
		instrumentationDisabled bool
		targetDisabled          bool
	}{
		{name: "annotation with default config", annotation: "app-config", want: "app-config"},
		{name: "annotation overrides central config", annotation: "app-config", central: "central-config", want: "app-config"},
		{name: "empty annotation keeps central config", central: "central-config", want: "central-config"},
		{name: "empty annotation keeps default config"},
		{name: "missing annotation keeps central config", missingAnnotation: true, central: "central-config", want: "central-config"},
		{name: "missing annotation keeps default config", missingAnnotation: true},
		{name: "annotation does not bypass pod selector", annotation: "app-config", podMismatch: true},
		{name: "annotation does not bypass namespace selector", annotation: "app-config", namespaceMismatch: true},
		{name: "annotation does not enable instrumentation", annotation: "app-config", instrumentationDisabled: true},
		{name: "annotation does not enable target", annotation: "app-config", targetDisabled: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			if err := monitoringv2alpha1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			cr := &monitoringv2alpha1.WhatapAgent{}
			cr.Name = "whatap"
			cr.Spec.Features.Apm.Instrumentation.Enabled = !tt.instrumentationDisabled
			config := monitoringv2alpha1.ConfigSpec{Mode: "default", VolumeName: "custom-config", MountPath: "/custom-config"}
			if tt.central != "" {
				config.Mode = "custom"
				config.ConfigMapRef = &monitoringv2alpha1.ConfigMapRef{Name: tt.central}
			}
			cr.Spec.Features.Apm.Instrumentation.Targets = []monitoringv2alpha1.TargetSpec{{
				Name: "nodejs", Enabled: !tt.targetDisabled, Language: "nodejs", Config: config,
				PodSelector: monitoringv2alpha1.PodSelector{MatchLabels: map[string]string{"app": "example"}},
			}}
			cr.Spec.Features.Apm.Instrumentation.Targets[0].NamespaceSelector.MatchNames = []string{"app"}
			if tt.namespaceMismatch {
				cr.Spec.Features.Apm.Instrumentation.Targets[0].NamespaceSelector.MatchNames = []string{"other"}
			}
			originalConfig := cr.DeepCopy().Spec.Features.Apm.Instrumentation.Targets[0].Config
			ns := &corev1.Namespace{}
			ns.Name = "app"
			k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr, ns).Build()
			recordingClient := &recordingWhatapAgentClient{Client: k8sClient}
			defaulter := &WhatapAgentCustomDefaulter{client: recordingClient}
			pod := &corev1.Pod{}
			pod.Name = "example"
			pod.Namespace = "app"
			if !tt.missingAnnotation {
				pod.Annotations = map[string]string{"whatap.io/apm-configmap": tt.annotation}
			}
			if !tt.podMismatch {
				pod.Labels = map[string]string{"app": "example"}
			}
			pod.Spec.Containers = []corev1.Container{{Name: "app", Image: "example:latest"}}
			if err := defaulter.Default(context.Background(), pod); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(recordingClient.resource.Spec.Features.Apm.Instrumentation.Targets[0].Config, originalConfig) {
				t.Fatal("in-memory central config changed")
			}
			got := ""
			for _, v := range pod.Spec.Volumes {
				if v.ConfigMap != nil {
					got = v.ConfigMap.Name
				}
			}
			if got != tt.want {
				t.Fatalf("ConfigMap = %q, want %q", got, tt.want)
			}
			if tt.want != "" {
				init := pod.Spec.InitContainers[0]
				if !strings.Contains(strings.Join(init.Args, " "), "cp /custom-config/whatap.conf /whatap-agent/") {
					t.Fatalf("init command does not copy whatap.conf: %v", init.Args)
				}
				found := false
				for _, m := range init.VolumeMounts {
					if m.Name == "custom-config" && m.MountPath == "/custom-config" {
						found = true
					}
				}
				if !found {
					t.Fatal("custom ConfigMap mount missing from init container")
				}
				for _, m := range pod.Spec.Containers[0].VolumeMounts {
					if m.Name == "custom-config" {
						t.Fatal("ConfigMap mounted directly into application")
					}
				}
			}
			stored := &monitoringv2alpha1.WhatapAgent{}
			if err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "whatap"}, stored); err != nil {
				t.Fatal(err)
			}
			storedConfig := stored.Spec.Features.Apm.Instrumentation.Targets[0].Config
			if storedConfig.Mode != config.Mode {
				t.Fatal("central config mode changed")
			}
			if tt.central == "" {
				if storedConfig.ConfigMapRef != nil {
					t.Fatal("central ConfigMap ref changed")
				}
			} else if storedConfig.ConfigMapRef.Name != tt.central {
				t.Fatal("central ConfigMap ref changed")
			}
		})
	}
}
