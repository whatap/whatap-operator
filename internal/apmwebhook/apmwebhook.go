// Package apmwebhook holds the identifiers shared by the controller that
// registers the APM Pod injection webhook and the webhook server behind it.
package apmwebhook

const (
	// PodInjectionPath is served by the Pod injection defaulter (mpod.kb.io).
	PodInjectionPath = "/whatap-injection--v1-pod"

	// ProbeWebhookName routes the operator's dry-run probe Pods to
	// PodInjectionPath. Unlike mpod.kb.io it fails closed, so the API server
	// returns its call error to the probe instead of admitting the Pod.
	ProbeWebhookName = "apm-probe.monitoring.whatap.com"

	// ProbeLabel marks the dry-run Pods the operator sends through the API
	// server. ProbeWebhookName selects only Pods carrying it.
	ProbeLabel = "monitoring.whatap.com/apm-webhook-probe"

	// ProbeReachedAnnotation is set by the injection handler on dry-run probe
	// Pods, proving that the API server called it.
	ProbeReachedAnnotation = "monitoring.whatap.com/apm-webhook-probe-reached"
)
