package v2alpha1

import (
	"context"
	"strings"
	"testing"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestKubeVirtProductionHandlerCarriesSelectedTarVersion(t *testing.T) {
	_, req, c := kubeVirtHandlerFixture(t)
	ctx := context.Background()
	policy := &monitoringv2alpha1.WhatapAgent{}
	if err := c.Get(ctx, client.ObjectKey{Name: "whatap"}, policy); err != nil {
		t.Fatal(err)
	}
	policy.Spec.Features.KubeVirt.Targets[0].AgentVersion = "2.9.26"
	if err := c.Update(ctx, policy); err != nil {
		t.Fatal(err)
	}
	response := newKubeVirtAdmissionHandler(c, c).Handle(ctx, req)
	if !response.Allowed || len(response.Patches) == 0 {
		t.Fatal("selected release prevented admission")
	}
	secrets := &corev1.SecretList{}
	if err := c.List(ctx, secrets, client.InNamespace("vms")); err != nil {
		t.Fatal(err)
	}
	for _, secret := range secrets.Items {
		if secret.Labels[bootstrapManagedLabel] == "true" && strings.Contains(string(secret.Data["userdata"]), "agent_version='2.9.26'") {
			return
		}
	}
	t.Fatal("production admission lost the selected tar release before writing the bootstrap Secret")
}

func TestKubeVirtInvalidTarVersionDoesNotCreateSecret(t *testing.T) {
	_, req, c := kubeVirtHandlerFixture(t)
	ctx := context.Background()
	policy := &monitoringv2alpha1.WhatapAgent{}
	if err := c.Get(ctx, client.ObjectKey{Name: "whatap"}, policy); err != nil {
		t.Fatal(err)
	}
	policy.Spec.Features.KubeVirt.Targets[0].AgentVersion = "2.9.25\n"
	if err := c.Update(ctx, policy); err != nil {
		t.Fatal(err)
	}
	response := newKubeVirtAdmissionHandler(c, c).Handle(ctx, req)
	if response.Allowed || len(response.Patches) != 0 {
		t.Fatal("unsafe release was admitted")
	}
	secrets := &corev1.SecretList{}
	if err := c.List(ctx, secrets, client.InNamespace("vms")); err != nil || len(secrets.Items) != 1 {
		t.Fatal("invalid release wrote a bootstrap Secret")
	}
}
