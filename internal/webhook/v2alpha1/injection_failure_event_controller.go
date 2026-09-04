package v2alpha1

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controllerevent "sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	injectionStatusAnnotation = "whatap-apm-injection-status"
	injectionReasonAnnotation = "whatap-apm-injection-reason"
	injectionStatusFailed     = "failed"
	injectionStatusSkipped    = "skipped"

	whatapAgentReadFailedReason = "WhatapAgentReadFailed"
	namespaceReadFailedReason   = "NamespaceReadFailed"

	injectionFailureEventComponent  = "whatap-apm-injector"
	injectionFailureEventRetryDelay = time.Second
)

var injectionFailureMessages = map[string]string{
	whatapAgentReadFailedReason: "Could not evaluate APM injection because the WhatapAgent CR could not be read. Check the whatap-operator logs for details.",
	namespaceReadFailedReason:   "Could not evaluate APM injection because the Pod namespace could not be read. Check the whatap-operator logs for details.",
}

// InjectionFailureEventReconciler records Warning Events only after the Pod has
// been persisted and therefore has its final name and UID.
type InjectionFailureEventReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create

func (r *InjectionFailureEventReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	pod := &metav1.PartialObjectMetadata{}
	pod.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "Pod"})
	if err := r.Get(ctx, req.NamespacedName, pod); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	reason, message, shouldRecord := injectionFailureDetails(pod)
	if !shouldRecord {
		return ctrl.Result{}, nil
	}
	if pod.GetName() == "" || pod.GetUID() == "" {
		return ctrl.Result{RequeueAfter: injectionFailureEventRetryDelay}, nil
	}

	now := metav1.Now()
	warningEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      injectionFailureEventName(pod.GetUID(), reason),
			Namespace: pod.GetNamespace(),
		},
		InvolvedObject: corev1.ObjectReference{
			APIVersion: "v1",
			Kind:       "Pod",
			Namespace:  pod.GetNamespace(),
			Name:       pod.GetName(),
			UID:        pod.GetUID(),
		},
		Reason:         reason,
		Message:        message,
		Source:         corev1.EventSource{Component: injectionFailureEventComponent},
		FirstTimestamp: now,
		LastTimestamp:  now,
		Count:          1,
		Type:           corev1.EventTypeWarning,
	}
	if err := r.Create(ctx, warningEvent); err != nil && !apierrors.IsAlreadyExists(err) {
		return ctrl.Result{}, fmt.Errorf("create APM injection failure event: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *InjectionFailureEventReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("apm_injection_failure_event").
		For(
			&corev1.Pod{},
			builder.OnlyMetadata,
			builder.WithPredicates(injectionFailureEventPredicate()),
		).
		Complete(r)
}

func injectionFailureEventPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e controllerevent.CreateEvent) bool {
			_, _, shouldRecord := injectionFailureDetails(e.Object)
			return shouldRecord
		},
		DeleteFunc: func(controllerevent.DeleteEvent) bool {
			return false
		},
		UpdateFunc: func(e controllerevent.UpdateEvent) bool {
			oldReason, _, oldFailure := injectionFailureDetails(e.ObjectOld)
			newReason, _, newFailure := injectionFailureDetails(e.ObjectNew)
			return newFailure && (!oldFailure || oldReason != newReason)
		},
		GenericFunc: func(controllerevent.GenericEvent) bool {
			return false
		},
	}
}

func injectionFailureDetails(obj client.Object) (reason, message string, shouldRecord bool) {
	if obj == nil {
		return "", "", false
	}
	annotations := obj.GetAnnotations()
	if annotations[injectionStatusAnnotation] != injectionStatusFailed {
		return "", "", false
	}
	reason = annotations[injectionReasonAnnotation]
	message, shouldRecord = injectionFailureMessages[reason]
	return reason, message, shouldRecord
}

func injectionFailureEventName(uid types.UID, reason string) string {
	digest := sha256.Sum256([]byte(string(uid) + "\x00" + reason))
	return fmt.Sprintf("whatap-apm-injection-%x", digest[:10])
}
