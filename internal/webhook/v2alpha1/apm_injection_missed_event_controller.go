package v2alpha1

import (
	"context"
	"fmt"
	"time"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controllerevent "sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	apmInjectedAnnotation     = "whatap-apm-injected"
	apmInjectionMissingReason = "APMInjectionMissing"
)

// MissedInjectionEventReconciler records a Warning Event on each Pod that
// matches an APM target but was admitted without the injection webhook.
// The webhook marks every Pod it evaluates (whatap-apm-injected or the
// injection status annotation) and mpod.kb.io fails open, so a matching Pod
// with neither was created while the API server could not call the webhook.
// Only Pods created after NotBefore (process start) are considered: operator
// versions before 3.0.19 did not mark skipped Pods, so an older unmarked Pod
// may simply predate its target.
type MissedInjectionEventReconciler struct {
	client.Client
	NotBefore time.Time
}

// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

func (r *MissedInjectionEventReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	pod := &metav1.PartialObjectMetadata{}
	pod.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "Pod"})
	if err := r.Get(ctx, req.NamespacedName, pod); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !missedInjectionCandidate(pod, r.NotBefore) {
		return ctrl.Result{}, nil
	}
	agent := &monitoringv2alpha1.WhatapAgent{}
	if err := r.Get(ctx, client.ObjectKey{Name: whatapAgentName}, agent); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !apmInjectionConfigured(agent) {
		return ctrl.Result{}, nil
	}
	namespace := &corev1.Namespace{}
	if err := r.Get(ctx, client.ObjectKey{Name: pod.GetNamespace()}, namespace); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	target, matched := matchingAPMTarget(agent.Spec.Features.Apm.Instrumentation.Targets, pod.GetLabels(), namespace)
	if !matched {
		return ctrl.Result{}, nil
	}

	whatapWebhookLogger.Info("Pod matching an APM target was created without APM injection because kube-apiserver did not call the injection webhook",
		"pod", req.NamespacedName.String(), "target", target)
	now := metav1.Now()
	warningEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      injectionFailureEventName(pod.GetUID(), apmInjectionMissingReason),
			Namespace: pod.GetNamespace(),
		},
		InvolvedObject: corev1.ObjectReference{
			APIVersion: "v1",
			Kind:       "Pod",
			Namespace:  pod.GetNamespace(),
			Name:       pod.GetName(),
			UID:        pod.GetUID(),
		},
		Reason: apmInjectionMissingReason,
		Message: fmt.Sprintf("APM injection was not applied: this Pod matches APM target %q, but kube-apiserver created it without calling "+
			"the WhaTap Operator webhook. See the events of WhatapAgent %q or the whatap-operator log for the cause, fix it, then restart the Pod.",
			target, whatapAgentName),
		Source:         corev1.EventSource{Component: injectionFailureEventComponent},
		FirstTimestamp: now,
		LastTimestamp:  now,
		Count:          1,
		Type:           corev1.EventTypeWarning,
	}
	if err := r.Create(ctx, warningEvent); err != nil && !apierrors.IsAlreadyExists(err) {
		return ctrl.Result{}, fmt.Errorf("create APM injection webhook event: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *MissedInjectionEventReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("apm_injection_webhook_not_called_event").
		For(
			&corev1.Pod{},
			builder.OnlyMetadata,
			builder.WithPredicates(missedInjectionPredicate(r.NotBefore)),
		).
		Complete(r)
}

// missedInjectionPredicate queues Pod creations only: admission is over once a
// Pod exists, so later updates cannot change whether it was injected.
func missedInjectionPredicate(notBefore time.Time) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e controllerevent.CreateEvent) bool {
			return missedInjectionCandidate(e.Object, notBefore)
		},
		UpdateFunc:  func(controllerevent.UpdateEvent) bool { return false },
		DeleteFunc:  func(controllerevent.DeleteEvent) bool { return false },
		GenericFunc: func(controllerevent.GenericEvent) bool { return false },
	}
}

// missedInjectionCandidate reports whether obj is a live Pod created at or after
// notBefore that the injection webhook did not mark.
func missedInjectionCandidate(obj client.Object, notBefore time.Time) bool {
	if obj == nil || obj.GetDeletionTimestamp() != nil {
		return false
	}
	// creationTimestamp has second precision.
	if obj.GetCreationTimestamp().Time.Before(notBefore.Truncate(time.Second)) {
		return false
	}
	annotations := obj.GetAnnotations()
	for _, mark := range []string{apmInjectedAnnotation, injectionStatusAnnotation, corev1.MirrorPodAnnotationKey} {
		if _, ok := annotations[mark]; ok {
			return false
		}
	}
	return true
}

// matchingAPMTarget returns the first enabled target that selects the Pod,
// using the selectors of the injection webhook.
func matchingAPMTarget(targets []monitoringv2alpha1.TargetSpec, podLabels map[string]string, namespace *corev1.Namespace) (string, bool) {
	for _, target := range targets {
		if target.Enabled && matchesSelector(podLabels, target.PodSelector) &&
			matchesNamespaceSelector(namespace.Name, namespace.Labels, target.NamespaceSelector) {
			return target.Name, true
		}
	}
	return "", false
}
