package v2alpha1

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// Real isolated API/etcd test, NOT a KubeVirt VM boot or guest installation.
func TestKubeVirtBootstrapWithLocalAPIServer(t *testing.T) {
	assets := os.Getenv("KUBEBUILDER_ASSETS")
	if assets == "" {
		t.Skip("set KUBEBUILDER_ASSETS to isolated envtest binaries")
	}
	useExisting := false
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = monitoringv2alpha1.AddToScheme(scheme)
	environment := &envtest.Environment{
		UseExistingCluster: &useExisting, BinaryAssetsDirectory: assets,
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true, Scheme: scheme,
		ControlPlaneStartTimeout: 40 * time.Second, ControlPlaneStopTimeout: 20 * time.Second,
	}
	cfg, err := environment.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Error(err)
		}
	})
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "vms"}}); err != nil {
		t.Fatal(err)
	}
	_, req, fixture := kubeVirtHandlerFixture(t)
	policy := &monitoringv2alpha1.WhatapAgent{}
	if err := fixture.Get(ctx, client.ObjectKey{Name: "whatap"}, policy); err != nil {
		t.Fatal(err)
	}
	policy.ObjectMeta = metav1.ObjectMeta{Name: "whatap"}
	policy.Spec.Features.KubeVirt.Targets[0].Port = 0
	if err := c.Create(ctx, policy); err != nil {
		t.Fatal(err)
	}
	storedPolicy := &monitoringv2alpha1.WhatapAgent{}
	if err := c.Get(ctx, client.ObjectKey{Name: "whatap"}, storedPolicy); err != nil {
		t.Fatal(err)
	}
	if !storedPolicy.Spec.Features.KubeVirt.Enabled || len(storedPolicy.Spec.Features.KubeVirt.Targets) != 1 || storedPolicy.Spec.Features.KubeVirt.Targets[0].Port != 6600 {
		t.Fatal("real CRD pruned policy or did not default collector port")
	}
	if storedPolicy.Spec.Features.KubeVirt.Targets[0].AgentVersion != "" {
		t.Fatal("real API persisted a default agentVersion; omitted targets must follow the operator default")
	}
	for _, version := range []string{"latest", "../2.9.25", "2.9.25\n"} {
		invalid := storedPolicy.DeepCopy()
		invalid.Spec.Features.KubeVirt.Targets[0].AgentVersion = version
		if err := c.Update(ctx, invalid); err == nil {
			t.Fatal("real API accepted an invalid tar release")
		}
	}
	license := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "license", Namespace: "vms"}, Data: map[string][]byte{"license": []byte("test-license-not-real")}}
	if err := c.Create(ctx, license); err != nil {
		t.Fatal(err)
	}
	h := newKubeVirtAdmissionHandler(c, c)
	for i := 0; i < 2; i++ {
		response := h.Handle(ctx, req)
		if !response.Allowed || len(response.Patches) == 0 {
			t.Fatalf("real-API bootstrap request failed: %+v", response.Result)
		}
	}
	secrets := &corev1.SecretList{}
	if err := c.List(ctx, secrets, client.InNamespace("vms")); err != nil {
		t.Fatal(err)
	}
	if len(secrets.Items) != 2 {
		t.Fatal("real API did not preserve immutable Secret idempotency")
	}
	for _, secret := range secrets.Items {
		if secret.Name == "license" {
			continue
		}
		if !ownedBootstrap(&secret, storedPolicy, "new-vm") {
			t.Fatal("real API did not preserve generated Secret ownership")
		}
		if secret.Annotations[bootstrapAdmittedAnnotation] == "" {
			t.Fatal("real API did not keep the refresh of the reused immutable Secret")
		}
	}

	// The CRD must accept and persist inline mode without requiring a source Secret.
	inline := storedPolicy.DeepCopy()
	inline.Spec.Features.KubeVirt.Targets[0].License = "inline-envtest-not-real=="
	inline.Spec.Features.KubeVirt.Targets[0].LicenseSecretRef = nil
	inline.Spec.Features.KubeVirt.Targets[0].AgentVersion = "2.9.26"
	if err := c.Update(ctx, inline); err != nil {
		t.Fatalf("CRD rejected inline license without licenseSecretRef: %v", err)
	}
	if err := c.Get(ctx, client.ObjectKey{Name: "whatap"}, storedPolicy); err != nil {
		t.Fatal(err)
	}
	if storedPolicy.Spec.Features.KubeVirt.Targets[0].License != "inline-envtest-not-real==" || storedPolicy.Spec.Features.KubeVirt.Targets[0].LicenseSecretRef != nil {
		t.Fatal("CRD pruned or changed inline license mode")
	}
	if storedPolicy.Spec.Features.KubeVirt.Targets[0].AgentVersion != "2.9.26" {
		t.Fatal("real API pruned or replaced the selected tar release")
	}
	if err := c.Delete(ctx, license); err != nil {
		t.Fatal(err)
	}
	req.Name = "new-vm-inline"
	req.Object.Raw = []byte(strings.ReplaceAll(string(req.Object.Raw), `"new-vm"`, `"new-vm-inline"`))
	response := h.Handle(ctx, req)
	if !response.Allowed || len(response.Patches) == 0 {
		t.Fatal("inline bootstrap requires a source license Secret")
	}
	if err := c.List(ctx, secrets, client.InNamespace("vms")); err != nil {
		t.Fatal(err)
	}
	if len(secrets.Items) != 2 {
		t.Fatal("expected one generated Secret for each VM, without a source license Secret")
	}
	foundInline := false
	for _, secret := range secrets.Items {
		if secret.Annotations[bootstrapVMAnnotation] == "new-vm-inline" {
			data := string(secret.Data["userdata"])
			foundInline = strings.Contains(data, "license=inline-envtest-not-real==") && strings.Contains(data, "agent_version='2.9.26'")
		}
	}
	if !foundInline {
		t.Fatal("real API did not store the inline bootstrap data")
	}
}
