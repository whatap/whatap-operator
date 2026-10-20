package controller

import (
	"github.com/whatap/whatap-operator/internal/apmwebhook"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// apmProbeWebhook sends the operator's dry-run probe Pods to the Pod injection
// handler. mpod.kb.io fails open, so when the API server cannot call the
// webhook (for example because a proxy intercepts its TLS connection and the
// certificate fails x509 verification) application Pods are admitted without
// APM injection and the error stays in the API server log. This entry fails
// closed, so the API server returns that error to the probe instead. Its
// object selector limits it to probe Pods; application Pods never match it.
func (r *WhatapAgentReconciler) apmProbeWebhook() admissionregistrationv1.MutatingWebhook {
	scope := admissionregistrationv1.NamespacedScope
	timeoutSeconds := int32(10)
	return admissionregistrationv1.MutatingWebhook{
		Name: apmwebhook.ProbeWebhookName,
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			Service: &admissionregistrationv1.ServiceReference{
				Name: webhookServiceName, Namespace: r.DefaultNamespace, Path: strPtr(apmwebhook.PodInjectionPath),
			},
			CABundle: r.WebhookCABundle,
		},
		Rules: []admissionregistrationv1.RuleWithOperations{{
			Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
			Rule: admissionregistrationv1.Rule{
				APIGroups: []string{""}, APIVersions: []string{"v1"},
				Resources: []string{"pods"}, Scope: &scope,
			},
		}},
		FailurePolicy: failurePtr(admissionregistrationv1.Fail),
		// The probe is a dry-run request; webhooks with side effects are not called for those.
		SideEffects: &sideEffectNone, AdmissionReviewVersions: []string{"v1"}, TimeoutSeconds: &timeoutSeconds,
		ObjectSelector: &metav1.LabelSelector{MatchLabels: map[string]string{apmwebhook.ProbeLabel: "true"}},
	}
}
