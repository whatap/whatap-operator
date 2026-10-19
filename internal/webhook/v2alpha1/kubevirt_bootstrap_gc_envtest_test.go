package v2alpha1

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	kv "github.com/whatap/whatap-operator/internal/kubevirt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// Real isolated API/etcd test of the collector's metadata-only requests and
// delete preconditions. No KubeVirt controller runs and no VM boots.
func TestKubeVirtBootstrapGCWithLocalAPIServer(t *testing.T) {
	assets := os.Getenv("KUBEBUILDER_ASSETS")
	if assets == "" {
		t.Skip("set KUBEBUILDER_ASSETS to isolated envtest binaries")
	}
	useExisting := false
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	environment := &envtest.Environment{
		UseExistingCluster: &useExisting, BinaryAssetsDirectory: assets,
		CRDDirectoryPaths:     []string{filepath.Join("testdata", "kubevirt-crds")},
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "vms"}}); err != nil {
		t.Fatal(err)
	}
	controller := true
	bootstrap := func(name, vmName, ownerKind string) *corev1.Secret {
		immutable := true
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: name, Namespace: "vms",
				Labels:      map[string]string{bootstrapManagedLabel: "true", "app.kubernetes.io/managed-by": "whatap-operator"},
				Annotations: map[string]string{bootstrapVMAnnotation: vmName},
			},
			Immutable: &immutable, Data: map[string][]byte{"userdata": []byte("#cloud-config\n# not a real license\n")},
		}
		switch ownerKind {
		case "WhatapAgent":
			secret.OwnerReferences = []metav1.OwnerReference{{APIVersion: "monitoring.whatap.com/v2alpha1", Kind: "WhatapAgent", Name: "whatap", UID: "policy-uid", Controller: &controller}}
		case "Deployment":
			secret.OwnerReferences = []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "other", UID: "other-uid", Controller: &controller}}
		}
		return secret
	}
	kubevirt := func(kind, name, marker, volumeSecret string) *unstructured.Unstructured {
		object := &unstructured.Unstructured{}
		object.SetAPIVersion("kubevirt.io/v1")
		object.SetKind(kind)
		object.SetNamespace("vms")
		object.SetName(name)
		if marker != "" {
			object.SetAnnotations(map[string]string{kv.InjectedAnnotation: marker})
		}
		if volumeSecret != "" {
			volumes := []interface{}{map[string]interface{}{"name": "cloudinit", "cloudInitNoCloud": map[string]interface{}{
				"secretRef": map[string]interface{}{"name": volumeSecret},
			}}}
			if err := unstructured.SetNestedSlice(object.Object, volumes, "spec", "template", "spec", "volumes"); err != nil {
				t.Fatal(err)
			}
		}
		return object
	}
	for _, object := range []client.Object{
		bootstrap("whatap-kv-used", "vm-used", "WhatapAgent"),
		bootstrap("whatap-kv-unmarked", "vm-unmarked", "WhatapAgent"),
		bootstrap("whatap-kv-gone", "vm-gone", "WhatapAgent"),
		bootstrap("whatap-kv-orphan", "vm-orphan", ""),
		bootstrap("whatap-kv-stale", "vm-stale", ""),
		bootstrap("whatap-kv-foreign", "vm-foreign", "Deployment"),
		kubevirt("VirtualMachine", "vm-used", "whatap-kv-used", "whatap-kv-used"),
		// The marker annotation was stripped, but the VM still mounts the Secret.
		kubevirt("VirtualMachine", "vm-unmarked", "", "whatap-kv-unmarked"),
		kubevirt("VirtualMachineInstance", "vm-orphan", "", ""),
	} {
		if err := c.Create(ctx, object); err != nil {
			t.Fatal(err)
		}
	}

	// Metadata-only list and a revision-preconditioned delete against the real API.
	store := apiBootstrapStore{reader: c, writer: c}
	listed, err := store.ListBootstrapSecrets(ctx)
	if err != nil || len(listed) != 6 {
		t.Fatalf("metadata list of generated Secrets failed: %d %v", len(listed), err)
	}
	var stale metav1.PartialObjectMetadata
	for _, item := range listed {
		if item.Name == "whatap-kv-stale" {
			stale = item
		}
	}
	if stale.GroupVersionKind() != corev1.SchemeGroupVersion.WithKind("Secret") || stale.UID == "" || stale.ResourceVersion == "" {
		t.Fatalf("listed metadata lacks kind or identity: %+v", stale.TypeMeta)
	}
	refreshed := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: "vms", Name: "whatap-kv-stale"}, refreshed); err != nil {
		t.Fatal(err)
	}
	base := refreshed.DeepCopy()
	metav1.SetMetaDataAnnotation(&refreshed.ObjectMeta, bootstrapAdmittedAnnotation, "refreshed")
	if err := c.Patch(ctx, refreshed, client.MergeFrom(base)); err != nil {
		t.Fatalf("immutable Secret metadata must accept a refresh: %v", err)
	}
	if deleted, err := store.DeleteSecret(ctx, &stale); deleted || err != nil {
		t.Fatalf("a revision changed after observation must not be deleted: %v %v", deleted, err)
	}
	if err := c.Get(ctx, client.ObjectKey{Namespace: "vms", Name: "whatap-kv-stale"}, &corev1.Secret{}); err != nil {
		t.Fatalf("precondition failure deleted the Secret: %v", err)
	}

	gc := newKubeVirtBootstrapGC(c, c)
	gc.now = func() time.Time { return time.Now().Add(time.Hour) }
	if result := gc.Sweep(ctx); result.Deleted != 0 || result.InUse != 3 || result.Pending != 2 || result.Skipped != 1 || len(result.Errors) != 0 {
		t.Fatalf("first sweep must only observe: %+v", result)
	}
	if result := gc.Sweep(ctx); result.Deleted != 2 || len(result.Errors) != 0 {
		t.Fatalf("second sweep must delete the unused Secrets: %+v", result)
	}
	for name, kept := range map[string]bool{
		"whatap-kv-used": true, "whatap-kv-unmarked": true, "whatap-kv-orphan": true, "whatap-kv-foreign": true,
		"whatap-kv-gone": false, "whatap-kv-stale": false,
	} {
		err := c.Get(ctx, client.ObjectKey{Namespace: "vms", Name: name}, &corev1.Secret{})
		if kept && err != nil {
			t.Fatalf("%s must be kept: %v", name, err)
		}
		if !kept && !apierrors.IsNotFound(err) {
			t.Fatalf("%s must be deleted: %v", name, err)
		}
	}
}
