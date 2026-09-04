package v2alpha1

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	controllerevent "sigs.k8s.io/controller-runtime/pkg/event"
)

func TestInjectionFailureEventReconcilerCreatesOneWarningEventForPersistedPod(t *testing.T) {
	pod := failedInjectionPod("example", "", types.UID("pod-uid"), "WhatapAgentReadFailed")
	k8sClient := newInjectionFailureTestClient(t, pod)
	reconciler := &InjectionFailureEventReconciler{Client: k8sClient}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)}

	for range 2 {
		if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
			t.Fatalf("Reconcile() error = %v", err)
		}
	}

	events := listInjectionFailureEvents(t, k8sClient, pod.Namespace)
	if got := len(events); got != 1 {
		t.Fatalf("event count = %d, want 1", got)
	}
	event := events[0]
	if event.Type != corev1.EventTypeWarning {
		t.Errorf("event type = %q, want Warning", event.Type)
	}
	if event.Reason != "WhatapAgentReadFailed" {
		t.Errorf("event reason = %q, want WhatapAgentReadFailed", event.Reason)
	}
	if event.Message == "" {
		t.Error("event message is empty")
	}
	if got := event.InvolvedObject; got.APIVersion != "v1" || got.Kind != "Pod" || got.Namespace != pod.Namespace || got.Name != pod.Name || got.UID != pod.UID {
		t.Errorf("event involvedObject = %#v, want persisted Pod identity", got)
	}
}

func TestInjectionFailureEventReconcilerUsesGeneratedPodsFinalIdentity(t *testing.T) {
	pod := failedInjectionPod("generated-abcde", "generated-", types.UID("generated-pod-uid"), "NamespaceReadFailed")
	k8sClient := newInjectionFailureTestClient(t, pod)
	reconciler := &InjectionFailureEventReconciler{Client: k8sClient}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)}); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	events := listInjectionFailureEvents(t, k8sClient, pod.Namespace)
	if got := len(events); got != 1 {
		t.Fatalf("event count = %d, want 1", got)
	}
	if got := events[0].InvolvedObject; got.Name != pod.Name || got.UID != pod.UID {
		t.Errorf("event involvedObject name/UID = %q/%q, want %q/%q", got.Name, got.UID, pod.Name, pod.UID)
	}
}

func TestInjectionFailureEventReconcilerIgnoresExpectedSkipAndUnknownFailure(t *testing.T) {
	testCases := []struct {
		name   string
		status string
		reason string
	}{
		{name: "expected skip", status: "skipped", reason: "NoMatchingTarget"},
		{name: "unknown failure", status: "failed", reason: "UserSuppliedReason"},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			pod := failedInjectionPod("example", "", types.UID("pod-uid"), tt.reason)
			pod.Annotations["whatap-apm-injection-status"] = tt.status
			k8sClient := newInjectionFailureTestClient(t, pod)
			reconciler := &InjectionFailureEventReconciler{Client: k8sClient}

			if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)}); err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}
			if got := len(listInjectionFailureEvents(t, k8sClient, pod.Namespace)); got != 0 {
				t.Fatalf("event count = %d, want 0", got)
			}
		})
	}
}

func TestInjectionFailureEventPredicateOnlyQueuesNewKnownFailures(t *testing.T) {
	predicate := injectionFailureEventPredicate()
	failedPod := failedInjectionPod("example", "", types.UID("pod-uid"), "NamespaceReadFailed")
	skippedPod := failedPod.DeepCopy()
	skippedPod.Annotations["whatap-apm-injection-status"] = "skipped"
	skippedPod.Annotations["whatap-apm-injection-reason"] = "NoMatchingTarget"

	if !predicate.Create(controllerevent.CreateEvent{Object: failedPod}) {
		t.Error("known failure create was not queued")
	}
	if predicate.Create(controllerevent.CreateEvent{Object: skippedPod}) {
		t.Error("expected skip create was queued")
	}
	if predicate.Update(controllerevent.UpdateEvent{ObjectOld: failedPod, ObjectNew: failedPod.DeepCopy()}) {
		t.Error("unchanged failure update was queued")
	}
	if !predicate.Update(controllerevent.UpdateEvent{ObjectOld: skippedPod, ObjectNew: failedPod}) {
		t.Error("new known failure update was not queued")
	}
}

func failedInjectionPod(name, generateName string, uid types.UID, reason string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:         name,
			GenerateName: generateName,
			Namespace:    "app",
			UID:          uid,
			Annotations: map[string]string{
				"whatap-apm-injection-status": "failed",
				"whatap-apm-injection-reason": reason,
			},
		},
	}
}

func newInjectionFailureTestClient(t *testing.T, pod *corev1.Pod) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
}

func listInjectionFailureEvents(t *testing.T, k8sClient client.Client, namespace string) []corev1.Event {
	t.Helper()
	eventList := &corev1.EventList{}
	if err := k8sClient.List(context.Background(), eventList, client.InNamespace(namespace)); err != nil {
		t.Fatal(err)
	}
	return eventList.Items
}
