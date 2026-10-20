package v2alpha1

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func missingInjectionFixture(t *testing.T) (*corev1.Pod, *monitoringv2alpha1.WhatapAgent, *corev1.Namespace) {
	t.Helper()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "app-abc", Namespace: "app", UID: "pod-uid", CreationTimestamp: metav1.NewTime(time.Now()), Labels: map[string]string{"app": "java"}}}
	cr := &monitoringv2alpha1.WhatapAgent{ObjectMeta: metav1.ObjectMeta{Name: "whatap", CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour))}}
	cr.Spec.Features.Apm.Instrumentation.Enabled = true
	cr.Spec.Features.Apm.Instrumentation.Targets = []monitoringv2alpha1.TargetSpec{{Name: "java-app", Enabled: true, Language: "java", PodSelector: monitoringv2alpha1.PodSelector{MatchLabels: map[string]string{"app": "java"}}, NamespaceSelector: monitoringv2alpha1.NamespaceSelector{MatchNames: []string{"app"}, MatchLabels: map[string]string{"monitor": "yes"}}}}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app", Labels: map[string]string{"monitor": "yes"}}}
	return pod, cr, ns
}

func missingInjectionClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := monitoringv2alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func TestMissingInjectionReportsPersistedPodOnce(t *testing.T) {
	pod, cr, ns := missingInjectionFixture(t)
	c := missingInjectionClient(t, pod, cr, ns)
	r := &MissingInjectionReconciler{Client: c, Reader: c}
	for range 2 {
		result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		if err != nil || result.RequeueAfter != missingInjectionInterval {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
	events := listInjectionFailureEvents(t, c, pod.Namespace)
	if len(events) != 1 {
		t.Fatalf("events=%d", len(events))
	}
	e := events[0]
	if e.Reason != missingInjectionReason || e.Type != corev1.EventTypeWarning || e.InvolvedObject.UID != pod.UID || !strings.Contains(e.Message, "current APM target") {
		t.Fatalf("unexpected event: %+v", e)
	}
	actual := &corev1.Pod{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(pod), actual); err != nil {
		t.Fatal(err)
	}
	if len(actual.Annotations) != 0 || len(actual.Spec.InitContainers) != 0 {
		t.Fatal("detection must not mutate Pod")
	}
}

func TestMissingInjectionExclusions(t *testing.T) {
	cases := []struct {
		name   string
		change func(*corev1.Pod, *monitoringv2alpha1.WhatapAgent, *corev1.Namespace)
	}{
		{"injected", func(p *corev1.Pod, c *monitoringv2alpha1.WhatapAgent, n *corev1.Namespace) {
			p.Annotations = map[string]string{"whatap-apm-injected": "true"}
		}},
		{"init without annotations", func(p *corev1.Pod, c *monitoringv2alpha1.WhatapAgent, n *corev1.Namespace) {
			p.Spec.InitContainers = []corev1.Container{{Name: InitContainerName}}
		}},
		{"skipped", func(p *corev1.Pod, c *monitoringv2alpha1.WhatapAgent, n *corev1.Namespace) {
			p.Annotations = map[string]string{injectionStatusAnnotation: injectionStatusSkipped}
		}},
		{"failed", func(p *corev1.Pod, c *monitoringv2alpha1.WhatapAgent, n *corev1.Namespace) {
			p.Annotations = map[string]string{injectionStatusAnnotation: injectionStatusFailed}
		}},
		{"reason only", func(p *corev1.Pod, c *monitoringv2alpha1.WhatapAgent, n *corev1.Namespace) {
			p.Annotations = map[string]string{injectionReasonAnnotation: "NoMatchingTarget"}
		}},
		{"mirror", func(p *corev1.Pod, c *monitoringv2alpha1.WhatapAgent, n *corev1.Namespace) {
			p.Annotations = map[string]string{corev1.MirrorPodAnnotationKey: "hash"}
		}},
		{"completed", func(p *corev1.Pod, c *monitoringv2alpha1.WhatapAgent, n *corev1.Namespace) {
			p.Status.Phase = corev1.PodSucceeded
		}},
		{"failed pod", func(p *corev1.Pod, c *monitoringv2alpha1.WhatapAgent, n *corev1.Namespace) {
			p.Status.Phase = corev1.PodFailed
		}},
		{"disabled", func(p *corev1.Pod, c *monitoringv2alpha1.WhatapAgent, n *corev1.Namespace) {
			c.Spec.Features.Apm.Instrumentation.Enabled = false
		}},
		{"target disabled", func(p *corev1.Pod, c *monitoringv2alpha1.WhatapAgent, n *corev1.Namespace) {
			c.Spec.Features.Apm.Instrumentation.Targets[0].Enabled = false
		}},
		{"no targets", func(p *corev1.Pod, c *monitoringv2alpha1.WhatapAgent, n *corev1.Namespace) {
			c.Spec.Features.Apm.Instrumentation.Targets = nil
		}},
		{"pod selector", func(p *corev1.Pod, c *monitoringv2alpha1.WhatapAgent, n *corev1.Namespace) { p.Labels["app"] = "other" }},
		{"namespace labels", func(p *corev1.Pod, c *monitoringv2alpha1.WhatapAgent, n *corev1.Namespace) {
			n.Labels["monitor"] = "no"
		}},
		{"namespace names", func(p *corev1.Pod, c *monitoringv2alpha1.WhatapAgent, n *corev1.Namespace) {
			c.Spec.Features.Apm.Instrumentation.Targets[0].NamespaceSelector.MatchNames = []string{"other"}
		}},
		{"pod before installation", func(p *corev1.Pod, c *monitoringv2alpha1.WhatapAgent, n *corev1.Namespace) {
			p.CreationTimestamp = metav1.NewTime(c.CreationTimestamp.Add(-time.Hour))
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			p, c, n := missingInjectionFixture(t)
			tt.change(p, c, n)
			cl := missingInjectionClient(t, p, c, n)
			r := &MissingInjectionReconciler{Client: cl, Reader: cl}
			if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)}); err != nil {
				t.Fatal(err)
			}
			if events := listInjectionFailureEvents(t, cl, p.Namespace); len(events) != 0 {
				t.Fatalf("unexpected events: %+v", events)
			}
		})
	}
}

func TestMissingInjectionReadFailureDoesNotReport(t *testing.T) {
	p, c, n := missingInjectionFixture(t)
	cl := missingInjectionClient(t, p, c, n)
	r := &MissingInjectionReconciler{Client: cl, Reader: failingGetClient{Client: cl, err: errors.New("forbidden")}}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)}); err == nil {
		t.Fatal("expected read error for retry")
	}
	if len(listInjectionFailureEvents(t, cl, p.Namespace)) != 0 {
		t.Fatal("read error reported as missing injection")
	}
}

func TestMissingInjectionRechecksConfiguration(t *testing.T) {
	p, c, n := missingInjectionFixture(t)
	c.Spec.Features.Apm.Instrumentation.Enabled = false
	cl := missingInjectionClient(t, p, c, n)
	r := &MissingInjectionReconciler{Client: cl, Reader: cl}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)}
	result, err := r.Reconcile(context.Background(), req)
	if err != nil || result.RequeueAfter == 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	c.Spec.Features.Apm.Instrumentation.Enabled = true
	if err := cl.Update(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(listInjectionFailureEvents(t, cl, p.Namespace)) != 1 {
		t.Fatal("changed configuration not evaluated")
	}
}

func TestMissingInjectionSelectorExpressionsAndLaterTargets(t *testing.T) {
	p, c, n := missingInjectionFixture(t)
	target := c.Spec.Features.Apm.Instrumentation.Targets[0]
	target.PodSelector.MatchExpressions = []monitoringv2alpha1.LabelSelectorRequirement{{Key: "app", Operator: "In", Values: []string{"java"}}, {Key: "excluded", Operator: "DoesNotExist"}}
	target.NamespaceSelector.MatchExpressions = []monitoringv2alpha1.LabelSelectorRequirement{{Key: "monitor", Operator: "Exists"}}
	c.Spec.Features.Apm.Instrumentation.Targets[0].NamespaceSelector.MatchNames = []string{"other"}
	c.Spec.Features.Apm.Instrumentation.Targets = append(c.Spec.Features.Apm.Instrumentation.Targets, target)
	cl := missingInjectionClient(t, p, c, n)
	r := &MissingInjectionReconciler{Client: cl, Reader: cl}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)}); err != nil {
		t.Fatal(err)
	}
	if len(listInjectionFailureEvents(t, cl, p.Namespace)) != 1 {
		t.Fatal("later matching target not detected")
	}
}

type missingEventFailureClient struct{ client.Client }

func (c missingEventFailureClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if _, ok := obj.(*corev1.Event); ok {
		return errors.New("event forbidden")
	}
	return c.Client.Create(ctx, obj, opts...)
}
func TestMissingInjectionEventFailureIsRetried(t *testing.T) {
	p, c, n := missingInjectionFixture(t)
	cl := missingInjectionClient(t, p, c, n)
	r := &MissingInjectionReconciler{Client: missingEventFailureClient{cl}, Reader: cl}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)}); err == nil {
		t.Fatal("event failure must be returned for retry")
	}
}

func TestMissingInjectionMissingCRAndDeletedPod(t *testing.T) {
	p, _, n := missingInjectionFixture(t)
	cl := missingInjectionClient(t, p, n)
	r := &MissingInjectionReconciler{Client: cl, Reader: cl}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)}
	if result, err := r.Reconcile(context.Background(), req); err != nil || result.RequeueAfter == 0 {
		t.Fatalf("missing CR: %+v %v", result, err)
	}
	if err := cl.Delete(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if result, err := r.Reconcile(context.Background(), req); err != nil || result.RequeueAfter != 0 {
		t.Fatalf("deleted Pod: %+v %v", result, err)
	}
	if len(listInjectionFailureEvents(t, cl, p.Namespace)) != 0 {
		t.Fatal("unexpected warning")
	}
}
