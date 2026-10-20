package v2alpha1

import (
	"context"
	"strings"
	"testing"
	"time"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	controllerevent "sigs.k8s.io/controller-runtime/pkg/event"
)

var missedInjectionStart = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

func TestMissedInjectionEventRecordsUnmarkedTargetPodOnce(t *testing.T) {
	pod := missedInjectionPod("demo-1", missedInjectionStart.Add(time.Minute), nil)
	k8sClient, reconciler := newMissedInjectionTest(t, apmTestAgent(true), pod)

	for range 2 {
		if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)}); err != nil {
			t.Fatalf("Reconcile() error = %v", err)
		}
	}
	events := listInjectionFailureEvents(t, k8sClient, "app")
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	event := events[0]
	if event.Type != corev1.EventTypeWarning || event.Reason != apmWebhookNotCalledReason {
		t.Fatalf("event = %s/%s, want Warning/%s", event.Type, event.Reason, apmWebhookNotCalledReason)
	}
	if !strings.Contains(event.Message, `APM target "demo"`) {
		t.Fatalf("event message does not name the target: %q", event.Message)
	}
	if got := event.InvolvedObject; got.Kind != "Pod" || got.Namespace != "app" || got.Name != pod.Name || got.UID != pod.UID {
		t.Fatalf("event involvedObject = %#v, want the Pod", got)
	}
}

func TestMissedInjectionEventIgnoresPodsTheWebhookEvaluated(t *testing.T) {
	notMatching := missedInjectionPod("other", missedInjectionStart.Add(time.Minute), nil)
	notMatching.Labels = map[string]string{"app": "other"}
	for name, testCase := range map[string]struct {
		agent *monitoringv2alpha1.WhatapAgent
		pod   *corev1.Pod
	}{
		"injected":                       {apmTestAgent(true), missedInjectionPod("injected", missedInjectionStart.Add(time.Minute), map[string]string{"whatap-apm-injected": "true"})},
		"skipped by the webhook":         {apmTestAgent(true), missedInjectionPod("skipped", missedInjectionStart.Add(time.Minute), map[string]string{injectionStatusAnnotation: injectionStatusSkipped})},
		"failed in the webhook":          {apmTestAgent(true), missedInjectionPod("failed", missedInjectionStart.Add(time.Minute), map[string]string{injectionStatusAnnotation: injectionStatusFailed})},
		"created before the operator":    {apmTestAgent(true), missedInjectionPod("old", missedInjectionStart.Add(-time.Minute), nil)},
		"not selected by any target":     {apmTestAgent(true), notMatching},
		"instrumentation disabled":       {apmTestAgent(false), missedInjectionPod("disabled", missedInjectionStart.Add(time.Minute), nil)},
		"no WhatapAgent":                 {nil, missedInjectionPod("no-agent", missedInjectionStart.Add(time.Minute), nil)},
		"static Pod mirrored by kubelet": {apmTestAgent(true), missedInjectionPod("mirror", missedInjectionStart.Add(time.Minute), map[string]string{corev1.MirrorPodAnnotationKey: "hash"})},
	} {
		t.Run(name, func(t *testing.T) {
			k8sClient, reconciler := newMissedInjectionTest(t, testCase.agent, testCase.pod)
			if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(testCase.pod)}); err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}
			if events := listInjectionFailureEvents(t, k8sClient, "app"); len(events) != 0 {
				t.Fatalf("events = %#v, want none", events)
			}
		})
	}
}

func TestMissedInjectionPredicateOnlyQueuesNewUnmarkedPodCreations(t *testing.T) {
	predicate := missedInjectionPredicate(missedInjectionStart)
	unmarked := missedInjectionPod("new", missedInjectionStart, nil)
	if !predicate.Create(controllerevent.CreateEvent{Object: unmarked}) {
		t.Error("a Pod created at operator start was not queued")
	}
	if predicate.Create(controllerevent.CreateEvent{Object: missedInjectionPod("injected", missedInjectionStart, map[string]string{"whatap-apm-injected": "true"})}) {
		t.Error("an injected Pod was queued")
	}
	if predicate.Create(controllerevent.CreateEvent{Object: missedInjectionPod("old", missedInjectionStart.Add(-time.Second), nil)}) {
		t.Error("a Pod created before operator start was queued")
	}
	deleting := unmarked.DeepCopy()
	now := metav1.NewTime(missedInjectionStart)
	deleting.DeletionTimestamp = &now
	if predicate.Create(controllerevent.CreateEvent{Object: deleting}) {
		t.Error("a terminating Pod was queued")
	}
	if predicate.Update(controllerevent.UpdateEvent{ObjectOld: unmarked, ObjectNew: unmarked.DeepCopy()}) {
		t.Error("a Pod update was queued")
	}
}

func missedInjectionPod(name string, created time.Time, annotations map[string]string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: "app", UID: types.UID(name + "-uid"),
		Labels:            map[string]string{"app": "demo"},
		Annotations:       annotations,
		CreationTimestamp: metav1.NewTime(created),
	}}
}

func newMissedInjectionTest(t *testing.T, agent *monitoringv2alpha1.WhatapAgent, pod *corev1.Pod) (client.Client, *MissedInjectionEventReconciler) {
	t.Helper()
	objects := []client.Object{pod, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app"}}}
	if agent != nil {
		objects = append(objects, agent)
	}
	k8sClient := fake.NewClientBuilder().WithScheme(newProbeTestScheme(t)).WithObjects(objects...).Build()
	return k8sClient, &MissedInjectionEventReconciler{Client: k8sClient, NotBefore: missedInjectionStart}
}
