package v2alpha1

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
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

func TestDefaultMarksUnexpectedWhatapAgentReadFailure(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	recorder := record.NewFakeRecorder(1)
	defaulter := &WhatapAgentCustomDefaulter{
		client: failingGetClient{
			Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
			err:    errors.New("forbidden"),
		},
		recorder: recorder,
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
	assertWarningEvent(t, recorder, "WhatapAgentReadFailed")
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

func TestDefaultDistinguishesNamespaceReadFailureFromSelectorMismatch(t *testing.T) {
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
	recorder := record.NewFakeRecorder(1)
	defaulter := &WhatapAgentCustomDefaulter{
		client:   fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr).Build(),
		recorder: recorder,
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
	assertWarningEvent(t, recorder, "NamespaceReadFailed")
}

func assertWarningEvent(t *testing.T, recorder *record.FakeRecorder, reason string) {
	t.Helper()
	select {
	case event := <-recorder.Events:
		if want := "Warning " + reason; len(event) < len(want) || event[:len(want)] != want {
			t.Errorf("event = %q, want prefix %q", event, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s event", reason)
	}
}
