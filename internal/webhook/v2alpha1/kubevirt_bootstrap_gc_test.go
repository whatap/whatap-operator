package v2alpha1

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

var gcNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

type fakeBootstrapStore struct {
	secrets       map[string]*metav1.PartialObjectMetadata
	vms           map[string][]string // name -> referenced Secrets, namespace "vms"
	vmis          map[string]bool
	secretListErr error
	vmListErr     error
	beforeDelete  func(key string)
	vmLists       int
	vmiLists      int
	deletes       []string
}

func newFakeBootstrapStore() *fakeBootstrapStore {
	return &fakeBootstrapStore{secrets: map[string]*metav1.PartialObjectMetadata{}, vms: map[string][]string{}, vmis: map[string]bool{}}
}

func (f *fakeBootstrapStore) ListBootstrapSecrets(context.Context) ([]metav1.PartialObjectMetadata, error) {
	if f.secretListErr != nil {
		return nil, f.secretListErr
	}
	keys := make([]string, 0, len(f.secrets))
	for key := range f.secrets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]metav1.PartialObjectMetadata, 0, len(keys))
	for _, key := range keys {
		out = append(out, *f.secrets[key].DeepCopy())
	}
	return out, nil
}

func (f *fakeBootstrapStore) ListVirtualMachines(context.Context, string) ([]bootstrapVM, error) {
	f.vmLists++
	if f.vmListErr != nil {
		return nil, f.vmListErr
	}
	var out []bootstrapVM
	for name, references := range f.vms {
		vm := bootstrapVM{Name: name, Secrets: map[string]struct{}{}}
		for _, reference := range references {
			vm.Secrets[reference] = struct{}{}
		}
		out = append(out, vm)
	}
	return out, nil
}

func (f *fakeBootstrapStore) ListVirtualMachineInstances(_ context.Context, namespace string) ([]metav1.PartialObjectMetadata, error) {
	f.vmiLists++
	var out []metav1.PartialObjectMetadata
	for name := range f.vmis {
		out = append(out, metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}})
	}
	return out, nil
}

func (f *fakeBootstrapStore) DeleteSecret(_ context.Context, secret *metav1.PartialObjectMetadata) (bool, error) {
	key := secret.Namespace + "/" + secret.Name
	if f.beforeDelete != nil {
		f.beforeDelete(key)
	}
	stored := f.secrets[key]
	if stored == nil || stored.UID != secret.UID || stored.ResourceVersion != secret.ResourceVersion {
		return false, nil
	}
	f.deletes = append(f.deletes, key)
	delete(f.secrets, key)
	return true, nil
}

func (f *fakeBootstrapStore) addSecret(name, vmName string, age time.Duration) *metav1.PartialObjectMetadata {
	controller := true
	secret := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: "vms", UID: types.UID("uid-" + name), ResourceVersion: "1",
		CreationTimestamp: metav1.NewTime(gcNow.Add(-age)),
		Labels:            map[string]string{bootstrapManagedLabel: "true", "app.kubernetes.io/managed-by": "whatap-operator"},
		Annotations:       map[string]string{bootstrapVMAnnotation: vmName},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: monitoringv2alpha1.GroupVersion.String(), Kind: "WhatapAgent", Name: "whatap", UID: "policy-uid", Controller: &controller,
		}},
	}}
	f.secrets["vms/"+name] = secret
	return secret
}

func gcFor(store bootstrapStore) *kubeVirtBootstrapGC {
	return &kubeVirtBootstrapGC{
		store: store, interval: time.Minute, minAge: bootstrapGCMinAge,
		now: func() time.Time { return gcNow }, log: logr.Discard(), unused: map[types.UID]string{},
	}
}

func TestBootstrapGCDeletesSecretOfDeletedVMOnSecondSweep(t *testing.T) {
	store := newFakeBootstrapStore()
	store.addSecret("whatap-kv-gone", "deleted-vm", time.Hour)
	gc := gcFor(store)
	if result := gc.Sweep(context.Background()); result.Deleted != 0 || result.Pending != 1 {
		t.Fatalf("first unused observation must not delete: %+v", result)
	}
	if result := gc.Sweep(context.Background()); result.Deleted != 1 || len(store.deletes) != 1 || store.deletes[0] != "vms/whatap-kv-gone" {
		t.Fatalf("second unused observation must delete the Secret: %+v %v", result, store.deletes)
	}
}

func TestBootstrapGCKeepsSecretsInUse(t *testing.T) {
	store := newFakeBootstrapStore()
	store.addSecret("whatap-kv-running", "vm-running", time.Hour)
	store.vms["vm-running"] = []string{"whatap-kv-running"}
	// The VM was deleted with orphan propagation; its VMI still runs and may migrate.
	store.addSecret("whatap-kv-orphan", "vm-orphan", time.Hour)
	store.vmis["vm-orphan"] = true
	// A same-name VM now uses another Secret: the old one is unused.
	store.addSecret("whatap-kv-replaced", "vm-replaced", time.Hour)
	store.vms["vm-replaced"] = []string{"whatap-kv-current"}
	store.vmis["vm-replaced"] = true
	gc := gcFor(store)
	for i := 0; i < 3; i++ {
		gc.Sweep(context.Background())
	}
	if store.secrets["vms/whatap-kv-running"] == nil || store.secrets["vms/whatap-kv-orphan"] == nil {
		t.Fatal("a Secret referenced by its VM or still used by a VMI was deleted")
	}
	if len(store.deletes) != 1 || store.deletes[0] != "vms/whatap-kv-replaced" {
		t.Fatalf("only the replaced Secret may be deleted: %v", store.deletes)
	}
	if store.vmLists != 3 || store.vmiLists != 3 {
		t.Fatalf("each namespace must be listed once per sweep: vm=%d vmi=%d", store.vmLists, store.vmiLists)
	}
}

func TestBootstrapGCKeepsYoungSecretsUntilMinAge(t *testing.T) {
	// A Secret whose VM CREATE is still in flight, or was rejected after admission.
	store := newFakeBootstrapStore()
	store.addSecret("whatap-kv-new", "creating-vm", time.Minute)
	gc := gcFor(store)
	for i := 0; i < 3; i++ {
		if result := gc.Sweep(context.Background()); result.Deleted != 0 || result.Pending != 1 {
			t.Fatalf("a Secret younger than the minimum age must be kept: %+v", result)
		}
	}
	gc.now = func() time.Time { return gcNow.Add(bootstrapGCMinAge) }
	gc.Sweep(context.Background())
	if result := gc.Sweep(context.Background()); result.Deleted != 1 {
		t.Fatalf("a Secret left by a rejected VM CREATE must be collected after the minimum age: %+v", result)
	}
}

func TestBootstrapGCStartsOverWhenAdmissionRefreshesSecret(t *testing.T) {
	store := newFakeBootstrapStore()
	secret := store.addSecret("whatap-kv-reused", "vm-reused", time.Hour)
	gc := gcFor(store)
	gc.Sweep(context.Background())
	// Admission reused the Secret for a re-created VM before the VM was persisted.
	secret.ResourceVersion = "2"
	if result := gc.Sweep(context.Background()); result.Deleted != 0 || result.Pending != 1 {
		t.Fatalf("a refreshed Secret must restart the observation: %+v", result)
	}
	// Admission refreshes it again between the list and the delete.
	store.beforeDelete = func(key string) { store.secrets[key].ResourceVersion = "3" }
	if result := gc.Sweep(context.Background()); result.Deleted != 0 || store.secrets["vms/whatap-kv-reused"] == nil {
		t.Fatalf("a Secret refreshed after the list must not be deleted: %+v", result)
	}
	store.beforeDelete = nil
	store.vms["vm-reused"] = []string{"whatap-kv-reused"}
	if result := gc.Sweep(context.Background()); result.InUse != 1 || len(store.deletes) != 0 {
		t.Fatalf("the persisted VM must keep its Secret: %+v", result)
	}
}

func TestBootstrapGCNeverDeletesOnUncertainty(t *testing.T) {
	store := newFakeBootstrapStore()
	store.addSecret("whatap-kv-lookup", "vm-lookup", time.Hour)
	store.addSecret("whatap-kv-noref", "", time.Hour)
	foreign := store.addSecret("whatap-kv-foreign", "vm-foreign", time.Hour)
	foreign.OwnerReferences[0].APIVersion, foreign.OwnerReferences[0].Kind = "kubevirt.io/v1", "VirtualMachine"
	store.addSecret("user-secret", "vm-user", time.Hour)
	unmanaged := store.addSecret("whatap-kv-unmanaged", "vm-unmanaged", time.Hour)
	delete(unmanaged.Labels, "app.kubernetes.io/managed-by")
	terminating := store.addSecret("whatap-kv-terminating", "vm-terminating", time.Hour)
	terminating.DeletionTimestamp = &metav1.Time{Time: gcNow}
	store.vmListErr = errors.New("forbidden")
	gc := gcFor(store)
	for i := 0; i < 3; i++ {
		result := gc.Sweep(context.Background())
		if result.Deleted != 0 || len(result.Errors) != 1 || result.Skipped != 5 {
			t.Fatalf("sweep %d: list failure, missing back-reference, foreign controller, name or label must be skipped: %+v", i, result)
		}
	}
	if store.vmiLists != 0 {
		t.Fatal("VMIs must not be consulted when VMs cannot be listed")
	}
	store.vmListErr = nil
	if result := gc.Sweep(context.Background()); result.Deleted != 0 {
		t.Fatalf("a failed lookup must not count as an unused observation: %+v", result)
	}
}

func TestBootstrapGCListFailureKeepsPreviousObservation(t *testing.T) {
	store := newFakeBootstrapStore()
	store.addSecret("whatap-kv-x", "vm-x", time.Hour)
	gc := gcFor(store)
	gc.Sweep(context.Background())
	store.secretListErr = errors.New("forbidden")
	if result := gc.Sweep(context.Background()); len(result.Errors) != 1 || len(store.deletes) != 0 {
		t.Fatalf("a list failure must not delete: %+v", result)
	}
	store.secretListErr = nil
	if result := gc.Sweep(context.Background()); result.Deleted != 1 {
		t.Fatalf("the unchanged revision seen before the failure must be collected: %+v", result)
	}
}

func TestReferencedSecretsCoversMarkerAndVolumeReferences(t *testing.T) {
	vm := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"annotations": map[string]interface{}{
			"monitoring.whatap.com/server-agent-bootstrap": "whatap-kv-marker",
		}},
		"spec": map[string]interface{}{"template": map[string]interface{}{"spec": map[string]interface{}{"volumes": []interface{}{
			map[string]interface{}{"name": "root", "persistentVolumeClaim": map[string]interface{}{"claimName": "root-disk"}},
			map[string]interface{}{"name": "cloudinit", "cloudInitNoCloud": map[string]interface{}{
				"secretRef": map[string]interface{}{"name": "whatap-kv-volume"}, "networkDataSecretRef": map[string]interface{}{"name": "network-data"},
			}},
			map[string]interface{}{"name": "drive", "cloudInitConfigDrive": map[string]interface{}{"secretRef": map[string]interface{}{"name": "config-drive"}}},
			map[string]interface{}{"name": "certs", "secret": map[string]interface{}{"secretName": "tls"}},
			map[string]interface{}{"name": "settings", "configMap": map[string]interface{}{"name": "not-a-secret"}},
		}}}},
	}}
	got := referencedSecrets(vm)
	want := []string{"config-drive", "network-data", "tls", "whatap-kv-marker", "whatap-kv-volume"}
	names := make([]string, 0, len(got))
	for name := range got {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) != len(want) {
		t.Fatalf("referenced Secrets = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("referenced Secrets = %v, want %v", names, want)
		}
	}
}

func TestBootstrapGCReportsRepeatedFailureHourly(t *testing.T) {
	gc := gcFor(newFakeBootstrapStore())
	failure := bootstrapGCResult{Errors: []error{errors.New("forbidden")}}
	gc.report(failure)
	first := gc.lastErrAt
	gc.now = func() time.Time { return gcNow.Add(bootstrapGCInterval) }
	gc.report(failure)
	if gc.lastErrAt != first {
		t.Fatal("the same failure was logged again within an hour")
	}
	gc.now = func() time.Time { return gcNow.Add(bootstrapGCErrorLogInterval) }
	gc.report(failure)
	if gc.lastErrAt == first {
		t.Fatal("a persisting failure must be logged again after an hour")
	}
}
