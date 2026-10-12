package v2alpha1

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestKubeVirtInlineLicenseBootstrapsWithoutSourceSecret(t *testing.T) {
	_, req, c := kubeVirtHandlerFixture(t)
	policy := &monitoringv2alpha1.WhatapAgent{}
	if err := c.Get(context.Background(), client.ObjectKey{Name: "whatap"}, policy); err != nil {
		t.Fatal(err)
	}
	// Decode from the public JSON surface so RED demonstrates dropped/missing
	// behavior rather than depending on a field which does not exist yet.
	policy.Spec.Features.KubeVirt.Targets = nil
	if err := json.Unmarshal([]byte(`[{"name":"linux","enabled":true,"namespace":"vms","vmSelector":{"matchLabels":{"app":"example"}},"license":"inline-test-license-not-real==","host":"collector.example","port":6600}]`), &policy.Spec.Features.KubeVirt.Targets); err != nil {
		t.Fatal(err)
	}
	if err := c.Update(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	license := &corev1.Secret{}
	if err := c.Get(context.Background(), client.ObjectKey{Name: "license", Namespace: "vms"}, license); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(context.Background(), license); err != nil {
		t.Fatal(err)
	}
	response := newKubeVirtAdmissionHandler(c, c).Handle(context.Background(), req)
	if !response.Allowed || len(response.Patches) == 0 {
		t.Fatalf("inline license cannot bootstrap without a source Secret: %+v", response.Result)
	}
	patch, err := json.Marshal(response.Patches)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(patch), "inline-test-license-not-real") {
		t.Fatal("inline license leaked into the VM patch")
	}
	secrets := &corev1.SecretList{}
	if err := c.List(context.Background(), secrets, client.InNamespace("vms")); err != nil {
		t.Fatal(err)
	}
	if len(secrets.Items) != 1 || secrets.Items[0].Labels[bootstrapManagedLabel] != "true" {
		t.Fatal("expected only the automatically generated bootstrap Secret")
	}
	if !strings.Contains(string(secrets.Items[0].Data["userdata"]), "license=inline-test-license-not-real==") {
		t.Fatal("inline license was not passed unchanged to cloud-init")
	}
}
