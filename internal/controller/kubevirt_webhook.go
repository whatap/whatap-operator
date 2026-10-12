package controller

import (
	kv "github.com/whatap/whatap-operator/internal/kubevirt"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// kubeVirtWebhook intentionally uses Fail only for explicitly opted-in VMs.
// A startup failure must not silently consume their first-boot installation window.
func (r *WhatapAgentReconciler) kubeVirtWebhook() admissionregistrationv1.MutatingWebhook {
	sideEffects := admissionregistrationv1.SideEffectClassNoneOnDryRun
	scope := admissionregistrationv1.NamespacedScope
	timeoutSeconds := int32(10)
	return admissionregistrationv1.MutatingWebhook{
		Name: kv.WebhookName,
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			Service: &admissionregistrationv1.ServiceReference{
				Name: webhookServiceName, Namespace: r.DefaultNamespace, Path: strPtr(kv.WebhookPath),
			},
			CABundle: r.WebhookCABundle,
		},
		Rules: []admissionregistrationv1.RuleWithOperations{{
			Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
			Rule: admissionregistrationv1.Rule{
				APIGroups: []string{"kubevirt.io"}, APIVersions: []string{"v1"},
				Resources: []string{"virtualmachines"}, Scope: &scope,
			},
		}},
		FailurePolicy: failurePtr(admissionregistrationv1.Fail),
		SideEffects:   &sideEffects, AdmissionReviewVersions: []string{"v1"}, TimeoutSeconds: &timeoutSeconds,
		ObjectSelector: &metav1.LabelSelector{MatchLabels: map[string]string{kv.OptInLabel: "true"}},
	}
}
