package v2alpha1

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	kv "github.com/whatap/whatap-operator/internal/kubevirt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// preparedVMRequest admits the fixture VM for real and returns a CREATE request
// for the prepared object (marker annotation plus Secret reference), as sent by
// `kubectl replace --force` or a restore of an exported VM.
func preparedVMRequest(t *testing.T, h *KubeVirtAdmissionHandler, req admission.Request) (admission.Request, string) {
	t.Helper()
	response := h.Handle(context.Background(), req)
	if !response.Allowed {
		t.Fatalf("fixture admission denied: %+v", response.Result)
	}
	encoded, _ := json.Marshal(response.Patches)
	patch, err := jsonpatch.DecodePatch(encoded)
	if err != nil {
		t.Fatal(err)
	}
	patched, err := patch.Apply(req.Object.Raw)
	if err != nil {
		t.Fatal(err)
	}
	var vm unstructured.Unstructured
	if err := vm.UnmarshalJSON(patched); err != nil {
		t.Fatal(err)
	}
	prepared := req
	prepared.Object.Raw = patched
	return prepared, vm.GetAnnotations()[kv.InjectedAnnotation]
}

func storedBootstrap(t *testing.T, c client.Client, name string) *corev1.Secret {
	t.Helper()
	secret := &corev1.Secret{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "vms", Name: name}, secret); err != nil {
		t.Fatal(err)
	}
	return secret
}

// fixturePolicy identifies the WhatapAgent of kubeVirtHandlerFixture.
func fixturePolicy() *monitoringv2alpha1.WhatapAgent {
	return &monitoringv2alpha1.WhatapAgent{ObjectMeta: metav1.ObjectMeta{Name: "whatap", UID: types.UID("policy-uid")}}
}

func TestKubeVirtAdmissionRefreshesReusedSecret(t *testing.T) {
	h, req, c := kubeVirtHandlerFixture(t)
	_, name := preparedVMRequest(t, h, req)
	created := storedBootstrap(t, c, name)
	if _, refreshed := created.Annotations[bootstrapAdmittedAnnotation]; refreshed {
		t.Fatal("a newly created Secret needs no refresh")
	}
	// The same VM is created again with identical content after a delete.
	if response := h.Handle(context.Background(), req); !response.Allowed {
		t.Fatalf("re-created VM denied: %+v", response.Result)
	}
	reused := storedBootstrap(t, c, name)
	if reused.Annotations[bootstrapAdmittedAnnotation] == "" || reused.ResourceVersion == created.ResourceVersion {
		t.Fatal("a reused Secret must be refreshed so the collector restarts its observation")
	}
	if string(reused.Data["userdata"]) != string(created.Data["userdata"]) || !ownedBootstrap(reused, fixturePolicy(), "new-vm") {
		t.Fatal("refresh changed the Secret content or ownership")
	}
}

func TestKubeVirtPreparedVMRefreshesOrExplainsCollectedSecret(t *testing.T) {
	h, req, c := kubeVirtHandlerFixture(t)
	prepared, name := preparedVMRequest(t, h, req)
	before := storedBootstrap(t, c, name)

	dry := true
	prepared.DryRun = &dry
	if response := h.Handle(context.Background(), prepared); !response.Allowed || storedBootstrap(t, c, name).ResourceVersion != before.ResourceVersion {
		t.Fatalf("dry-run of a prepared VM must be allowed without writes: %+v", response.Result)
	}
	prepared.DryRun = nil
	if response := h.Handle(context.Background(), prepared); !response.Allowed || len(response.Patches) != 0 {
		t.Fatalf("prepared VM denied or mutated: %+v", response.Result)
	}
	if storedBootstrap(t, c, name).Annotations[bootstrapAdmittedAnnotation] == "" {
		t.Fatal("a prepared VM reusing its Secret must refresh it")
	}

	if err := c.Delete(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "vms", Name: name}}); err != nil {
		t.Fatal(err)
	}
	response := h.Handle(context.Background(), prepared)
	if response.Allowed || !strings.Contains(response.Result.Message, "no longer exists") {
		t.Fatalf("a prepared VM whose Secret was collected must be denied with guidance: %+v", response.Result)
	}
}

func TestKubeVirtAdmissionRecreatesSecretCollectedDuringReuse(t *testing.T) {
	h, req, c := kubeVirtHandlerFixture(t)
	_, name := preparedVMRequest(t, h, req)
	collected := false
	intercepted := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		Patch: func(ctx context.Context, inner client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if !collected && obj.GetName() == name {
				// The collector deletes the Secret between admission's read and refresh.
				collected = true
				if err := inner.Delete(ctx, obj.DeepCopyObject().(client.Object)); err != nil {
					return err
				}
			}
			return inner.Patch(ctx, obj, patch, opts...)
		},
	})
	h.Reader, h.Writer = intercepted, intercepted
	if response := h.Handle(context.Background(), req); !response.Allowed {
		t.Fatalf("re-created VM denied: %+v", response.Result)
	}
	if !collected {
		t.Fatal("test did not exercise the collection race")
	}
	if secret := storedBootstrap(t, c, name); !ownedBootstrap(secret, fixturePolicy(), "new-vm") {
		t.Fatal("a Secret collected during admission must be created again")
	}
}

func TestKubeVirtAdmissionAllowsWhenRefreshIsForbidden(t *testing.T) {
	h, req, c := kubeVirtHandlerFixture(t)
	_, name := preparedVMRequest(t, h, req)
	intercepted := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
			return apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, name, nil)
		},
	})
	h.Reader, h.Writer = intercepted, intercepted
	if response := h.Handle(context.Background(), req); !response.Allowed {
		t.Fatalf("a verified Secret must be reused even without patch permission: %+v", response.Result)
	}
}
