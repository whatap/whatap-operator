package v2alpha1

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	kv "github.com/whatap/whatap-operator/internal/kubevirt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	bootstrapSecretPrefix = "whatap-kv-"
	bootstrapGCInterval   = 5 * time.Minute
	// bootstrapGCMinAge leaves a just-created Secret alone while the API server
	// is still persisting, or rejecting, the VirtualMachine admitted with it.
	bootstrapGCMinAge           = 10 * time.Minute
	bootstrapGCPageSize         = 500
	bootstrapGCErrorLogInterval = time.Hour
)

var (
	secretListGVK                 = corev1.SchemeGroupVersion.WithKind("SecretList")
	virtualMachineListGVK         = schema.GroupVersionKind{Group: "kubevirt.io", Version: "v1", Kind: "VirtualMachineList"}
	virtualMachineInstanceListGVK = schema.GroupVersionKind{Group: "kubevirt.io", Version: "v1", Kind: "VirtualMachineInstanceList"}
)

// +kubebuilder:rbac:groups="",resources=secrets,verbs=list;delete
// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines;virtualmachineinstances,verbs=list

// bootstrapVM is the part of a VirtualMachine the collector needs: every Secret
// it references through the bootstrap marker or a volume.
type bootstrapVM struct {
	Name    string
	Secrets map[string]struct{}
}

// bootstrapStore is the API surface of the collector. Secrets are read as
// metadata only, so generated cloud-init payloads (which carry the access key)
// never enter it.
type bootstrapStore interface {
	ListBootstrapSecrets(ctx context.Context) ([]metav1.PartialObjectMetadata, error)
	ListVirtualMachines(ctx context.Context, namespace string) ([]bootstrapVM, error)
	ListVirtualMachineInstances(ctx context.Context, namespace string) ([]metav1.PartialObjectMetadata, error)
	// DeleteSecret deletes exactly the observed revision. It reports false when
	// the Secret is already gone or has changed since it was observed.
	DeleteSecret(ctx context.Context, secret *metav1.PartialObjectMetadata) (bool, error)
}

// kubeVirtBootstrapGC deletes generated cloud-init Secrets that no VirtualMachine
// uses any more. Admission creates each Secret before its VM exists, so the VM
// cannot own it, and garbage collection would keep it until the WhatapAgent CR
// is deleted. A Secret is in use while the VM named in its bootstrap-vm
// annotation still references it (by marker or by any volume) or, once that VM
// is gone, while a VMI of the same name exists (for example after an orphaning
// delete).
//
// Deletion is conservative. A Secret must be older than bootstrapGCMinAge and
// be seen unused, at the same resourceVersion, on two consecutive sweeps; the
// delete is preconditioned on that UID and revision. Admission refreshes any
// Secret it reuses, so a VM re-created with identical content keeps it. Secrets
// without a back-reference, Secrets controlled by anything other than a
// WhatapAgent, and namespaces whose VMs cannot be listed are left alone.
type kubeVirtBootstrapGC struct {
	store    bootstrapStore
	interval time.Duration
	minAge   time.Duration
	now      func() time.Time
	log      logr.Logger

	// unused maps the UID of each Secret seen unused on the previous sweep to
	// the resourceVersion it had then.
	unused    map[types.UID]string
	lastErr   string
	lastErrAt time.Time
}

type bootstrapGCResult struct {
	Deleted int
	Pending int
	InUse   int
	Skipped int
	Errors  []error
}

func newKubeVirtBootstrapGC(reader client.Reader, writer client.Writer) *kubeVirtBootstrapGC {
	return &kubeVirtBootstrapGC{
		store:    apiBootstrapStore{reader: reader, writer: writer},
		interval: bootstrapGCInterval,
		minAge:   bootstrapGCMinAge,
		now:      time.Now,
		log:      whatapWebhookLogger.WithName("kubevirt-bootstrap-gc"),
		unused:   map[types.UID]string{},
	}
}

// NeedLeaderElection keeps the collector on a single replica.
func (g *kubeVirtBootstrapGC) NeedLeaderElection() bool { return true }

// Start sweeps immediately and then every interval until ctx is done.
func (g *kubeVirtBootstrapGC) Start(ctx context.Context) error {
	ticker := time.NewTicker(g.interval)
	defer ticker.Stop()
	for {
		g.report(g.Sweep(ctx))
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// bootstrapNamespace caches one namespace's VM state for a single sweep.
type bootstrapNamespace struct {
	vms     map[string]map[string]struct{} // VM name -> Secrets it references
	vmis    map[string]struct{}
	listErr error
}

// Sweep performs one pass over the generated Secrets.
func (g *kubeVirtBootstrapGC) Sweep(ctx context.Context) bootstrapGCResult {
	var result bootstrapGCResult
	secrets, err := g.store.ListBootstrapSecrets(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("list KubeVirt bootstrap Secrets: %w", err))
		return result
	}
	now := g.now()
	unused := make(map[types.UID]string)
	namespaces := make(map[string]*bootstrapNamespace)
	for i := range secrets {
		secret := &secrets[i]
		vmName, ok := collectableBootstrap(secret)
		if !ok {
			result.Skipped++
			continue
		}
		state := namespaces[secret.Namespace]
		if state == nil {
			state = &bootstrapNamespace{}
			namespaces[secret.Namespace] = state
		}
		inUse, err := g.inUse(ctx, state, secret.Namespace, vmName, secret.Name)
		if err != nil {
			result.Errors = append(result.Errors, err)
			continue
		}
		if inUse {
			result.InUse++
			continue
		}
		if now.Sub(secret.CreationTimestamp.Time) < g.minAge {
			result.Pending++
			continue
		}
		if revision, seen := g.unused[secret.UID]; !seen || revision != secret.ResourceVersion {
			unused[secret.UID] = secret.ResourceVersion
			result.Pending++
			continue
		}
		deleted, err := g.store.DeleteSecret(ctx, secret)
		if err != nil {
			unused[secret.UID] = secret.ResourceVersion
			result.Errors = append(result.Errors, fmt.Errorf("delete Secret %s/%s: %w", secret.Namespace, secret.Name, err))
			continue
		}
		if !deleted {
			// Changed or removed since it was listed; the next sweep starts over.
			result.Pending++
			continue
		}
		result.Deleted++
		g.log.Info("deleted unused KubeVirt bootstrap Secret", "namespace", secret.Namespace, "secret", secret.Name, "virtualMachine", vmName)
	}
	g.unused = unused
	return result
}

func (g *kubeVirtBootstrapGC) inUse(ctx context.Context, state *bootstrapNamespace, namespace, vmName, secretName string) (bool, error) {
	if state.listErr != nil {
		return false, state.listErr
	}
	if state.vms == nil {
		vms, err := g.store.ListVirtualMachines(ctx, namespace)
		if err != nil {
			state.listErr = fmt.Errorf("list VirtualMachines in %s: %w", namespace, err)
			return false, state.listErr
		}
		state.vms = make(map[string]map[string]struct{}, len(vms))
		for _, vm := range vms {
			state.vms[vm.Name] = vm.Secrets
		}
	}
	if references, found := state.vms[vmName]; found {
		_, referenced := references[secretName]
		return referenced, nil
	}
	if state.vmis == nil {
		vmis, err := g.store.ListVirtualMachineInstances(ctx, namespace)
		if err != nil {
			state.listErr = fmt.Errorf("list VirtualMachineInstances in %s: %w", namespace, err)
			return false, state.listErr
		}
		state.vmis = make(map[string]struct{}, len(vmis))
		for _, vmi := range vmis {
			state.vmis[vmi.Name] = struct{}{}
		}
	}
	_, running := state.vmis[vmName]
	return running, nil
}

func (g *kubeVirtBootstrapGC) report(result bootstrapGCResult) {
	if len(result.Errors) == 0 {
		return
	}
	// A missing permission repeats every sweep; log the same failure at most hourly.
	message, now := result.Errors[0].Error(), g.now()
	if message == g.lastErr && now.Sub(g.lastErrAt) < bootstrapGCErrorLogInterval {
		return
	}
	g.lastErr, g.lastErrAt = message, now
	g.log.Error(result.Errors[0], "KubeVirt bootstrap Secret cleanup incomplete; affected Secrets are kept", "failures", len(result.Errors))
}

// collectableBootstrap returns the VM a generated Secret was rendered for. A
// Secret without that back-reference or with a foreign controller is never
// collected.
func collectableBootstrap(secret *metav1.PartialObjectMetadata) (string, bool) {
	if !strings.HasPrefix(secret.Name, bootstrapSecretPrefix) ||
		secret.Labels[bootstrapManagedLabel] != "true" ||
		secret.Labels["app.kubernetes.io/managed-by"] != "whatap-operator" ||
		secret.DeletionTimestamp != nil {
		return "", false
	}
	if owner := metav1.GetControllerOfNoCopy(secret); owner != nil &&
		(owner.Kind != "WhatapAgent" || owner.APIVersion != monitoringv2alpha1.GroupVersion.String()) {
		return "", false
	}
	vmName := secret.Annotations[bootstrapVMAnnotation]
	return vmName, vmName != ""
}

// referencedSecrets returns the bootstrap marker and every Secret a VM template
// volume points to, so a VM whose marker was removed still keeps its Secret.
func referencedSecrets(vm *unstructured.Unstructured) map[string]struct{} {
	references := make(map[string]struct{})
	if marker := vm.GetAnnotations()[kv.InjectedAnnotation]; marker != "" {
		references[marker] = struct{}{}
	}
	volumes, _, _ := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "volumes")
	for _, item := range volumes {
		volume, _ := item.(map[string]interface{})
		for _, source := range volume {
			fields, _ := source.(map[string]interface{})
			for key, value := range fields {
				switch typed := value.(type) {
				case map[string]interface{}:
					// secretRef, networkDataSecretRef and similar references.
					if name, _ := typed["name"].(string); name != "" && strings.HasSuffix(strings.ToLower(key), "secretref") {
						references[name] = struct{}{}
					}
				case string:
					if key == "secretName" && typed != "" {
						references[typed] = struct{}{}
					}
				}
			}
		}
	}
	return references
}

type apiBootstrapStore struct {
	reader client.Reader
	writer client.Writer
}

func (s apiBootstrapStore) ListBootstrapSecrets(ctx context.Context) ([]metav1.PartialObjectMetadata, error) {
	return s.list(ctx, secretListGVK, client.MatchingLabels{bootstrapManagedLabel: "true"})
}

// ListVirtualMachines reads whole VMs because their volume references are spec.
func (s apiBootstrapStore) ListVirtualMachines(ctx context.Context, namespace string) ([]bootstrapVM, error) {
	var vms []bootstrapVM
	next := ""
	for {
		page := &unstructured.UnstructuredList{}
		page.SetGroupVersionKind(virtualMachineListGVK)
		options := []client.ListOption{client.InNamespace(namespace), client.Limit(bootstrapGCPageSize)}
		if next != "" {
			options = append(options, client.Continue(next))
		}
		if err := s.reader.List(ctx, page, options...); err != nil {
			return nil, err
		}
		for i := range page.Items {
			vms = append(vms, bootstrapVM{Name: page.Items[i].GetName(), Secrets: referencedSecrets(&page.Items[i])})
		}
		if next = page.GetContinue(); next == "" {
			return vms, nil
		}
	}
}

func (s apiBootstrapStore) ListVirtualMachineInstances(ctx context.Context, namespace string) ([]metav1.PartialObjectMetadata, error) {
	return s.list(ctx, virtualMachineInstanceListGVK, client.InNamespace(namespace))
}

func (s apiBootstrapStore) list(ctx context.Context, listGVK schema.GroupVersionKind, filter client.ListOption) ([]metav1.PartialObjectMetadata, error) {
	itemGVK := listGVK
	itemGVK.Kind = strings.TrimSuffix(listGVK.Kind, "List")
	var items []metav1.PartialObjectMetadata
	next := ""
	for {
		page := &metav1.PartialObjectMetadataList{}
		page.SetGroupVersionKind(listGVK)
		options := []client.ListOption{filter, client.Limit(bootstrapGCPageSize)}
		if next != "" {
			options = append(options, client.Continue(next))
		}
		if err := s.reader.List(ctx, page, options...); err != nil {
			return nil, err
		}
		for i := range page.Items {
			item := page.Items[i]
			// Metadata list items carry no kind; restore it for later requests.
			item.SetGroupVersionKind(itemGVK)
			items = append(items, item)
		}
		if next = page.Continue; next == "" {
			return items, nil
		}
	}
}

func (s apiBootstrapStore) DeleteSecret(ctx context.Context, secret *metav1.PartialObjectMetadata) (bool, error) {
	uid, revision := secret.UID, secret.ResourceVersion
	err := s.writer.Delete(ctx, secret, client.Preconditions{UID: &uid, ResourceVersion: &revision})
	switch {
	case err == nil:
		return true, nil
	case apierrors.IsNotFound(err) || apierrors.IsConflict(err):
		return false, nil
	default:
		return false, err
	}
}
