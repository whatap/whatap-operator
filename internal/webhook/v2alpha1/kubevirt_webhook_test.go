package v2alpha1

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	"github.com/whatap/whatap-operator/internal/kubevirt/cloudinit"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func kubeVirtHandlerFixture(t *testing.T) (*KubeVirtAdmissionHandler, admission.Request, client.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = monitoringv2alpha1.AddToScheme(scheme)
	cr := &monitoringv2alpha1.WhatapAgent{ObjectMeta: metav1.ObjectMeta{Name: "whatap", UID: types.UID("policy-uid")}}
	cr.Spec.Features.KubeVirt = monitoringv2alpha1.KubeVirtSpec{Enabled: true, Targets: []monitoringv2alpha1.KubeVirtTargetSpec{{
		Name: "linux", Enabled: true, Namespace: "vms", VMSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "example"}},
		LicenseSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "license"}, Key: "license"},
		Host:             "collector.example", Port: 6600,
	}}}
	license := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "license", Namespace: "vms"}, Data: map[string][]byte{"license": []byte("test-license-not-real")}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr, license).Build()
	h := &KubeVirtAdmissionHandler{Reader: c, Writer: c, Render: func(existing []byte, config cloudinit.Config) ([]byte, error) {
		if config.License != "test-license-not-real" || config.Host != "collector.example" || config.Port != 6600 || config.AgentVersion != "2.9.25" {
			return nil, errors.New("bad renderer inputs")
		}
		return []byte("#cloud-config\n# renderer fixture\n# " + config.License + "\n" + string(existing)), nil
	}}
	raw := []byte(`{"apiVersion":"kubevirt.io/v1","kind":"VirtualMachine","metadata":{"name":"new-vm","namespace":"vms","labels":{"monitoring.whatap.com/server-agent":"true","app":"example"}},"spec":{"runStrategy":"Always","template":{"metadata":{"labels":{"app":"example"}},"spec":{"domain":{"devices":{"disks":[{"name":"root","disk":{"bus":"virtio"}}]}},"volumes":[{"name":"root","persistentVolumeClaim":{"claimName":"root-disk"}},{"name":"cloudinit","cloudInitNoCloud":{"userData":"#cloud-config\ntimezone: Asia/Seoul\n","networkData":"version: 2\nethernets: {}"}}]}}}}`)
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{UID: "request-uid", Kind: metav1.GroupVersionKind{Group: "kubevirt.io", Version: "v1", Kind: "VirtualMachine"}, Resource: metav1.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachines"}, Namespace: "vms", Name: "new-vm", Operation: admissionv1.Create, Object: runtime.RawExtension{Raw: raw}}}
	return h, req, c
}

func TestKubeVirtAdmissionCreatesSecretWithoutLeakingLicenseInPatch(t *testing.T) {
	h, req, c := kubeVirtHandlerFixture(t)
	response := h.Handle(context.Background(), req)
	if !response.Allowed || len(response.Patches) == 0 {
		t.Fatalf("missing injection: %+v", response.Result)
	}
	patch, _ := json.Marshal(response.Patches)
	if strings.Contains(string(patch), "test-license-not-real") {
		t.Fatal("license leaked into VM patch")
	}
	if !strings.Contains(string(patch), "secretRef") {
		t.Fatal("cloud-init must use secretRef, not inline data")
	}
	secrets := &corev1.SecretList{}
	if err := c.List(context.Background(), secrets, client.InNamespace("vms")); err != nil {
		t.Fatal(err)
	}
	if len(secrets.Items) != 2 {
		t.Fatalf("secret count=%d, want source plus generated", len(secrets.Items))
	}
	for _, secret := range secrets.Items {
		if secret.Name == "license" {
			continue
		}
		if secret.Immutable == nil || !*secret.Immutable || len(secret.OwnerReferences) != 1 || secret.OwnerReferences[0].UID != "policy-uid" {
			t.Fatal("generated Secret lacks immutability or policy ownership")
		}
		if !strings.Contains(string(secret.Data["userdata"]), "timezone: Asia/Seoul") {
			t.Fatal("original userdata was lost")
		}
	}
	// Repeated requests must reuse immutable content, not generate credential copies.
	again := h.Handle(context.Background(), req)
	if !again.Allowed {
		t.Fatalf("repeat denied: %+v", again.Result)
	}
	_ = c.List(context.Background(), secrets, client.InNamespace("vms"))
	if len(secrets.Items) != 2 {
		t.Fatal("non-idempotent generated Secret")
	}
	if !strings.Contains(string(patch), "bootstrap") {
		t.Fatal("missing prepared-state annotation")
	}
}

func TestKubeVirtAdmissionDryRunHasNoWrites(t *testing.T) {
	h, req, c := kubeVirtHandlerFixture(t)
	value := true
	req.DryRun = &value
	response := h.Handle(context.Background(), req)
	if !response.Allowed || len(response.Patches) == 0 {
		t.Fatal("dry-run should return the predicted mutation")
	}
	secrets := &corev1.SecretList{}
	_ = c.List(context.Background(), secrets)
	if len(secrets.Items) != 1 {
		t.Fatal("dry-run created a Secret")
	}
}

func TestKubeVirtAdmissionNeverModifiesExistingVMsOrPods(t *testing.T) {
	for _, kind := range []string{"update", "pod", "vmi", "subresource", "not opted in"} {
		t.Run(kind, func(t *testing.T) {
			h, req, c := kubeVirtHandlerFixture(t)
			switch kind {
			case "update":
				req.Operation = admissionv1.Update
			case "pod":
				req.Resource = metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
			case "vmi":
				req.Resource.Resource = "virtualmachineinstances"
			case "subresource":
				req.SubResource = "status"
			case "not opted in":
				req.Object.Raw = []byte(strings.Replace(string(req.Object.Raw), `"monitoring.whatap.com/server-agent":"true"`, `"monitoring.whatap.com/server-agent":"false"`, 1))
			}
			resp := h.Handle(context.Background(), req)
			if !resp.Allowed || len(resp.Patches) != 0 {
				t.Fatal("out-of-scope admission was changed")
			}
			secrets := &corev1.SecretList{}
			_ = c.List(context.Background(), secrets)
			if len(secrets.Items) != 1 {
				t.Fatal("out-of-scope request created secret")
			}
		})
	}
}
