package controller

import (
	"context"
	"testing"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	"github.com/whatap/whatap-operator/internal/apmwebhook"
	kv "github.com/whatap/whatap-operator/internal/kubevirt"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestKubeVirtWebhookIsOptInCreateOnlyAndRemovedWhenDisabled(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = monitoringv2alpha1.AddToScheme(scheme)
	_ = admissionregistrationv1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &WhatapAgentReconciler{Client: c, Scheme: scheme, DefaultNamespace: "whatap-monitoring", WebhookCABundle: []byte("test-ca")}
	cr := &monitoringv2alpha1.WhatapAgent{ObjectMeta: metav1.ObjectMeta{Name: "whatap", UID: "policy-uid"}}
	for _, enabled := range []bool{false, true, true, false} {
		cr.Spec.Features.KubeVirt.Enabled = enabled
		if err := r.ensureMutatingWebhookConfiguration(context.Background(), cr); err != nil {
			t.Fatal(err)
		}
		mwc := &admissionregistrationv1.MutatingWebhookConfiguration{}
		if err := c.Get(context.Background(), client.ObjectKey{Name: webhookConfigurationName}, mwc); err != nil {
			t.Fatal(err)
		}
		want := 3
		if enabled {
			want = 4
		}
		if len(mwc.Webhooks) != want {
			t.Fatalf("enabled=%v hooks=%d want=%d", enabled, len(mwc.Webhooks), want)
		}
		if mwc.Webhooks[0].Name != "mpod.kb.io" || mwc.Webhooks[1].Name != "whatapagent.kb.io" {
			t.Fatal("existing webhook identities changed")
		}
		if mwc.Webhooks[want-1].Name != apmwebhook.ProbeWebhookName {
			t.Fatal("APM probe webhook must stay registered whether KubeVirt is enabled or not")
		}
		if !enabled {
			continue
		}
		wh := mwc.Webhooks[2]
		if wh.Name != kv.WebhookName || wh.ClientConfig.Service.Path == nil || *wh.ClientConfig.Service.Path != kv.WebhookPath {
			t.Fatal("bootstrap webhook not wired")
		}
		if wh.SideEffects == nil || *wh.SideEffects != admissionregistrationv1.SideEffectClassNoneOnDryRun {
			t.Fatal("Secret-writing admission must declare NoneOnDryRun")
		}
		if wh.ObjectSelector == nil || wh.ObjectSelector.MatchLabels[kv.OptInLabel] != "true" {
			t.Fatal("webhook can block unrelated VMs")
		}
		if wh.FailurePolicy == nil || *wh.FailurePolicy != admissionregistrationv1.Fail {
			t.Fatal("opted-in VM can silently boot without installation")
		}
		if len(wh.Rules) != 1 || len(wh.Rules[0].Operations) != 1 || wh.Rules[0].Operations[0] != admissionregistrationv1.Create || len(wh.Rules[0].Resources) != 1 || wh.Rules[0].Resources[0] != "virtualmachines" {
			t.Fatal("webhook is not VM CREATE only")
		}
	}
}
