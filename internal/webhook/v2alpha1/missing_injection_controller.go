package v2alpha1

import (
	"context"
	"fmt"
	"maps"
	"time"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const missingInjectionReason = "APMInjectionMissing"
const missingInjectionInterval = 5 * time.Minute

// MissingInjectionReconciler observes persisted Pods independently of admission.
// Keep the existing metadata-only Pod cache; read specs directly only for targets.
type MissingInjectionReconciler struct {
	client.Client
	Reader client.Reader
}

// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get
func (r *MissingInjectionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	meta := &metav1.PartialObjectMetadata{}
	meta.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "Pod"})
	if err := r.Get(ctx, req.NamespacedName, meta); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !missingInjectionCandidate(meta) {
		return ctrl.Result{}, nil
	}
	// Re-evaluate current selectors even when only the CR/namespace changes.
	retry := ctrl.Result{RequeueAfter: missingInjectionInterval}
	cr := &monitoringv2alpha1.WhatapAgent{}
	if err := r.Get(ctx, client.ObjectKey{Name: "whatap"}, cr); err != nil {
		return retry, client.IgnoreNotFound(err)
	}
	instrumentation := cr.Spec.Features.Apm.Instrumentation
	if !instrumentation.Enabled || len(instrumentation.Targets) == 0 {
		return retry, nil
	}
	// Pods predating installation cannot establish that admission was missed.
	if meta.CreationTimestamp.Before(&cr.CreationTimestamp) {
		return retry, nil
	}
	var targetName string
	matched := false
	for _, target := range instrumentation.Targets {
		if !target.Enabled || !matchesSelector(meta.Labels, target.PodSelector) {
			continue
		}
		ns := &corev1.Namespace{}
		if err := r.Reader.Get(ctx, client.ObjectKey{Name: meta.Namespace}, ns); err != nil {
			return ctrl.Result{}, err
		}
		if matchesNamespaceSelector(meta.Namespace, ns.Labels, target.NamespaceSelector) {
			targetName, matched = target.Name, true
			break
		}
	}
	if !matched {
		return retry, nil
	}
	pod := &corev1.Pod{}
	if err := r.Reader.Get(ctx, req.NamespacedName, pod); err != nil {
		return retry, client.IgnoreNotFound(err)
	}
	// Recheck identity/metadata after the uncached read, including label changes.
	if pod.UID != meta.UID || !missingInjectionCandidate(pod) {
		return retry, nil
	}
	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return ctrl.Result{}, nil
	}
	for _, container := range pod.Spec.InitContainers {
		if container.Name == InitContainerName {
			return ctrl.Result{}, nil
		}
	}
	// A concurrent label change will be evaluated by the next pass.
	if !maps.Equal(pod.Labels, meta.Labels) {
		return retry, nil
	}
	now := metav1.Now()
	event := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: injectionFailureEventName(pod.UID, missingInjectionReason), Namespace: pod.Namespace},
		InvolvedObject: corev1.ObjectReference{APIVersion: "v1", Kind: "Pod", Namespace: pod.Namespace, Name: pod.Name, UID: pod.UID},
		Reason:         missingInjectionReason,
		Message:        fmt.Sprintf("Pod matches current APM target %q but has no Whatap agent init container or injection result annotation. Admission may have been bypassed or failed; check API server webhook errors, TLS/proxy connectivity and whether target selectors changed after Pod creation. This observation does not establish the failure cause.", targetName),
		Source:         corev1.EventSource{Component: injectionFailureEventComponent},
		Type:           corev1.EventTypeWarning, FirstTimestamp: now, LastTimestamp: now, Count: 1,
	}
	if err := r.Create(ctx, event); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return retry, nil
		}
		return ctrl.Result{}, fmt.Errorf("create missing APM injection event: %w", err)
	}
	ctrl.LoggerFrom(ctx).Info("APM injection missing", "reason", missingInjectionReason, "pod", req.NamespacedName, "target", targetName, "message", event.Message)
	return retry, nil
}

func missingInjectionCandidate(obj client.Object) bool {
	if obj == nil || obj.GetUID() == "" || obj.GetDeletionTimestamp() != nil {
		return false
	}
	annotations := obj.GetAnnotations()
	return annotations["whatap-apm-injected"] != "true" && annotations[injectionStatusAnnotation] == "" && annotations[injectionReasonAnnotation] == "" && annotations[corev1.MirrorPodAnnotationKey] == ""
}

func (r *MissingInjectionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("apm_missing_injection").
		For(&corev1.Pod{}, builder.OnlyMetadata, builder.WithPredicates(predicate.NewPredicateFuncs(missingInjectionCandidate))).
		Complete(r)
}
