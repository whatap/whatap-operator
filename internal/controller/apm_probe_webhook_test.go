package controller

import (
	"context"
	"testing"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	"github.com/whatap/whatap-operator/internal/apmwebhook"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAPMProbeWebhookFailsClosedForProbePodsOnly(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = monitoringv2alpha1.AddToScheme(scheme)
	_ = admissionregistrationv1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &WhatapAgentReconciler{Client: c, Scheme: scheme, DefaultNamespace: "whatap-monitoring", WebhookCABundle: []byte("test-ca")}
	cr := &monitoringv2alpha1.WhatapAgent{ObjectMeta: metav1.ObjectMeta{Name: "whatap", UID: "policy-uid"}}
	if err := r.ensureMutatingWebhookConfiguration(context.Background(), cr); err != nil {
		t.Fatal(err)
	}
	mwc := &admissionregistrationv1.MutatingWebhookConfiguration{}
	if err := c.Get(context.Background(), client.ObjectKey{Name: webhookConfigurationName}, mwc); err != nil {
		t.Fatal(err)
	}
	var mpod, probe *admissionregistrationv1.MutatingWebhook
	for i := range mwc.Webhooks {
		switch mwc.Webhooks[i].Name {
		case "mpod.kb.io":
			mpod = &mwc.Webhooks[i]
		case apmwebhook.ProbeWebhookName:
			probe = &mwc.Webhooks[i]
		}
	}
	if mpod == nil || probe == nil {
		t.Fatal("Pod injection or APM probe webhook missing")
	}
	if mpod.FailurePolicy == nil || *mpod.FailurePolicy != admissionregistrationv1.Ignore {
		t.Fatal("application Pods must keep failing open")
	}
	if probe.FailurePolicy == nil || *probe.FailurePolicy != admissionregistrationv1.Fail {
		t.Fatal("probe must fail closed to surface the API server's call error")
	}
	if probe.ObjectSelector == nil || len(probe.ObjectSelector.MatchExpressions) != 0 ||
		len(probe.ObjectSelector.MatchLabels) != 1 || probe.ObjectSelector.MatchLabels[apmwebhook.ProbeLabel] != "true" {
		t.Fatal("fail-closed probe webhook can block application Pods")
	}
	if probe.SideEffects == nil || *probe.SideEffects != admissionregistrationv1.SideEffectClassNone {
		t.Fatal("dry-run probe requests are only sent to webhooks without side effects")
	}
	// The probe must exercise the same TLS endpoint as application Pods.
	if probe.ClientConfig.Service == nil || mpod.ClientConfig.Service == nil ||
		*probe.ClientConfig.Service.Path != apmwebhook.PodInjectionPath || *mpod.ClientConfig.Service.Path != apmwebhook.PodInjectionPath ||
		probe.ClientConfig.Service.Name != mpod.ClientConfig.Service.Name ||
		probe.ClientConfig.Service.Namespace != mpod.ClientConfig.Service.Namespace ||
		string(probe.ClientConfig.CABundle) != string(mpod.ClientConfig.CABundle) {
		t.Fatal("probe webhook does not share the Pod injection endpoint and CA")
	}
	if len(probe.Rules) != 1 || len(probe.Rules[0].Operations) != 1 || probe.Rules[0].Operations[0] != admissionregistrationv1.Create ||
		len(probe.Rules[0].Resources) != 1 || probe.Rules[0].Resources[0] != "pods" {
		t.Fatal("probe webhook is not Pod CREATE only")
	}
}
