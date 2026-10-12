package v2alpha1

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"

	"fmt"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	kv "github.com/whatap/whatap-operator/internal/kubevirt"
	"github.com/whatap/whatap-operator/internal/kubevirt/cloudinit"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	maxOriginalUserData    = 256 * 1024
	maxRenderedUserData    = 512 * 1024
	bootstrapManagedLabel  = "monitoring.whatap.com/kubevirt-bootstrap"
	bootstrapVMAnnotation  = "monitoring.whatap.com/bootstrap-vm"
	defaultCloudInitVolume = "whatap-cloudinit"
)

// +kubebuilder:rbac:groups="",resources=secrets,verbs=create;get

// KubeVirtAdmissionHandler prepares a new VM before its first VMI exists.
// All generated credential-bearing data stays in a namespace-local Secret.
// It writes only during real CREATE admissions: registration must declare
// SideEffects=NoneOnDryRun. No VM/VMI watches or KubeVirt client are required.
type KubeVirtAdmissionHandler struct {
	Reader client.Reader
	Writer client.Client
	Render func(existing []byte, config cloudinit.Config) ([]byte, error)
}

func newKubeVirtAdmissionHandler(reader client.Reader, writer client.Client) *KubeVirtAdmissionHandler {
	return &KubeVirtAdmissionHandler{
		Reader: reader,
		Writer: writer,
		Render: cloudinit.Render,
	}
}

func (h *KubeVirtAdmissionHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	if req.Operation != admissionv1.Create || req.SubResource != "" || req.Resource.Group != "kubevirt.io" || req.Resource.Version != "v1" || req.Resource.Resource != "virtualmachines" {
		return admission.Allowed("outside new VirtualMachine scope")
	}
	vm := &unstructured.Unstructured{}
	if err := vm.UnmarshalJSON(req.Object.Raw); err != nil {
		return admission.Denied("invalid VirtualMachine JSON")
	}
	if vm.GetAPIVersion() != "kubevirt.io/v1" || vm.GetKind() != "VirtualMachine" {
		return admission.Denied("unexpected VirtualMachine kind")
	}
	if vm.GetLabels()[kv.OptInLabel] != "true" {
		return admission.Allowed("server agent bootstrap not requested")
	}
	if vm.GetNamespace() == "" {
		vm.SetNamespace(req.Namespace)
	}
	if vm.GetName() == "" || vm.GetNamespace() != req.Namespace {
		return admission.Denied("VirtualMachine name/namespace is required and must match admission")
	}
	policy := &monitoringv2alpha1.WhatapAgent{}
	if err := h.Reader.Get(ctx, client.ObjectKey{Name: "whatap"}, policy); err != nil {
		return admission.Denied("cannot read WhatapAgent bootstrap policy")
	}
	target, err := kv.SelectTarget(policy.Spec.Features.KubeVirt, vm.GetNamespace(), vm.GetLabels())
	if err != nil {
		return admission.Denied(err.Error())
	}
	if target == nil {
		return admission.Allowed("no enabled KubeVirt target matches")
	}
	if policy.UID == "" || !policy.DeletionTimestamp.IsZero() {
		return admission.Denied("WhatapAgent bootstrap policy is not ready")
	}
	volumes, source, volumeIndex, sourceType, err := cloudInitSource(vm)
	if err != nil {
		return admission.Denied(err.Error())
	}
	if marker := vm.GetAnnotations()[kv.InjectedAnnotation]; marker != "" {
		ref, _, _ := unstructured.NestedString(source, "secretRef", "name")
		if marker != ref {
			return admission.Denied("bootstrap marker does not match cloud-init Secret")
		}
		prepared := &corev1.Secret{}
		if err := h.Reader.Get(ctx, client.ObjectKey{Namespace: vm.GetNamespace(), Name: ref}, prepared); err != nil || !ownedBootstrap(prepared, policy, vm.GetName()) {
			return admission.Denied("bootstrap marker is not backed by an owned immutable Secret")
		}
		if prepared.Name != bootstrapSecretName(policy, vm, prepared.Data["userdata"]) {
			return admission.Denied("bootstrap Secret content does not match its identity")
		}
		return admission.Allowed("cloud-init bootstrap already prepared; guest installation is not verified")
	}
	existing, err := h.readUserData(ctx, vm.GetNamespace(), source)
	if err != nil {
		return admission.Denied(err.Error())
	}
	key := target.License
	if ref := target.LicenseSecretRef; ref != nil {
		license := &corev1.Secret{}
		if err := h.Reader.Get(ctx, client.ObjectKey{Namespace: vm.GetNamespace(), Name: ref.Name}, license); err != nil {
			return admission.Denied("cannot read server license Secret in the VM namespace")
		}
		value, ok := license.Data[ref.Key]
		if !ok || len(value) == 0 {
			return admission.Denied("server license Secret key is missing or empty")
		}
		key = string(value)
	}
	if h.Render == nil {
		return admission.Denied("cloud-init renderer is unavailable")
	}
	userData, err := h.Render(existing, cloudinit.Config{License: key, Host: target.Host, Port: target.Port, AgentVersion: target.AgentVersion})
	if err != nil {
		// Renderer errors must be secret-safe; do not surface parser excerpts from user data.
		return admission.Denied("cannot safely merge cloud-init configuration; check supported formats and reserved paths")
	}
	if len(userData) == 0 || len(userData) > maxRenderedUserData {
		return admission.Denied("rendered cloud-init data exceeds the supported size")
	}
	secret := newBootstrapSecret(policy, vm, userData)
	delete(source, "userData")
	delete(source, "userDataBase64")
	source["secretRef"] = map[string]interface{}{"name": secret.Name}
	volume := volumes[volumeIndex].(map[string]interface{})
	volume[sourceType] = source
	if err := unstructured.SetNestedSlice(vm.Object, volumes, "spec", "template", "spec", "volumes"); err != nil {
		return admission.Denied("invalid VM volume structure")
	}
	if err := ensureCloudInitDisk(vm, volume["name"].(string)); err != nil {
		return admission.Denied(err.Error())
	}
	annotations := vm.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[kv.InjectedAnnotation] = secret.Name
	annotations[kv.TargetAnnotation] = target.Name
	vm.SetAnnotations(annotations)
	mutated, err := vm.MarshalJSON()
	if err != nil {
		return admission.Denied("cannot encode the prepared VirtualMachine")
	}
	response := admission.PatchResponseFromRaw(req.Object.Raw, mutated)
	if !response.Allowed {
		return response
	}
	if req.DryRun == nil || !*req.DryRun {
		if err := h.ensureBootstrapSecret(ctx, secret, policy, vm.GetName()); err != nil {
			return admission.Denied("cannot prepare owned cloud-init Secret; no existing Secret was overwritten")
		}
	}
	return response
}

func cloudInitSource(vm *unstructured.Unstructured) ([]interface{}, map[string]interface{}, int, string, error) {
	if _, found, err := unstructured.NestedMap(vm.Object, "spec", "template", "spec"); err != nil || !found {
		return nil, nil, 0, "", fmt.Errorf("VirtualMachine spec.template.spec is required")
	}
	volumes, _, err := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "volumes")
	if err != nil {
		return nil, nil, 0, "", fmt.Errorf("invalid VM volumes")
	}
	index := -1
	var source map[string]interface{}
	var kind string
	for i, raw := range volumes {
		volume, ok := raw.(map[string]interface{})
		if !ok {
			return nil, nil, 0, "", fmt.Errorf("invalid VM volume")
		}
		name, ok := volume["name"].(string)
		if !ok || name == "" {
			return nil, nil, 0, "", fmt.Errorf("VM volumes must have names")
		}
		for _, sourceKind := range []string{"cloudInitNoCloud", "cloudInitConfigDrive"} {
			value, exists := volume[sourceKind]
			if !exists {
				continue
			}
			if index >= 0 {
				return nil, nil, 0, "", fmt.Errorf("multiple cloud-init sources are not supported")
			}
			var valid bool
			source, valid = value.(map[string]interface{})
			if !valid {
				return nil, nil, 0, "", fmt.Errorf("invalid cloud-init source")
			}
			index, kind = i, sourceKind
		}
	}
	if index < 0 {
		for _, raw := range volumes {
			if raw.(map[string]interface{})["name"] == defaultCloudInitVolume {
				return nil, nil, 0, "", fmt.Errorf("reserved cloud-init volume name is already in use")
			}
		}
		source = make(map[string]interface{})
		index, kind = len(volumes), "cloudInitNoCloud"
		volumes = append(volumes, map[string]interface{}{"name": defaultCloudInitVolume, kind: source})
	}
	return volumes, source, index, kind, nil
}

func (h *KubeVirtAdmissionHandler) readUserData(ctx context.Context, namespace string, source map[string]interface{}) ([]byte, error) {
	var data []byte
	count := 0
	for _, field := range []string{"userData", "userDataBase64", "secretRef"} {
		value, exists := source[field]
		if !exists {
			continue
		}
		count++
		switch field {
		case "userData":
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("invalid inline cloud-init data")
			}
			data = []byte(text)
		case "userDataBase64":
			text, ok := value.(string)
			if !ok || len(text) > base64.StdEncoding.EncodedLen(maxOriginalUserData) {
				return nil, fmt.Errorf("invalid encoded cloud-init data")
			}
			var err error
			data, err = base64.StdEncoding.DecodeString(text)
			if err != nil {
				return nil, fmt.Errorf("invalid encoded cloud-init data")
			}
		case "secretRef":
			name, found, err := unstructured.NestedString(source, "secretRef", "name")
			if err != nil || !found || name == "" {
				return nil, fmt.Errorf("cloud-init Secret name is required")
			}
			secret := &corev1.Secret{}
			if err := h.Reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, secret); err != nil {
				return nil, fmt.Errorf("cannot read original cloud-init Secret")
			}
			// These are the two keys accepted by KubeVirt's cloud-init loader.
			var ok bool
			data, ok = secret.Data["userdata"]
			if !ok {
				data, ok = secret.Data["userData"]
			}
			if !ok {
				return nil, fmt.Errorf("cloud-init Secret must contain userdata")
			}
		}
	}
	if count > 1 {
		return nil, fmt.Errorf("multiple cloud-init user data forms cannot be combined")
	}
	if len(data) > maxOriginalUserData {
		return nil, fmt.Errorf("original cloud-init data exceeds the supported size")
	}
	return data, nil
}

func ensureCloudInitDisk(vm *unstructured.Unstructured, name string) error {
	disks, _, err := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "domain", "devices", "disks")
	if err != nil {
		return fmt.Errorf("invalid VM disk structure")
	}
	for _, raw := range disks {
		disk, ok := raw.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid VM disk")
		}
		if disk["name"] == name {
			return nil
		}
	}
	disks = append(disks, map[string]interface{}{"name": name, "disk": map[string]interface{}{"bus": "virtio"}})
	if err := unstructured.SetNestedSlice(vm.Object, disks, "spec", "template", "spec", "domain", "devices", "disks"); err != nil {
		return fmt.Errorf("invalid VM disk structure")
	}
	return nil
}

func bootstrapSecretName(policy *monitoringv2alpha1.WhatapAgent, vm *unstructured.Unstructured, data []byte) string {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "%s\x00%s\x00%s\x00", policy.UID, vm.GetNamespace(), vm.GetName())
	_, _ = hash.Write(data)
	return "whatap-kv-" + hex.EncodeToString(hash.Sum(nil))[:40]
}

func newBootstrapSecret(policy *monitoringv2alpha1.WhatapAgent, vm *unstructured.Unstructured, data []byte) *corev1.Secret {
	immutable, controller := true, true
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: bootstrapSecretName(policy, vm, data), Namespace: vm.GetNamespace(),
			Labels:          map[string]string{bootstrapManagedLabel: "true", "app.kubernetes.io/managed-by": "whatap-operator"},
			Annotations:     map[string]string{bootstrapVMAnnotation: vm.GetName()},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: monitoringv2alpha1.GroupVersion.String(), Kind: "WhatapAgent", Name: policy.Name, UID: policy.UID, Controller: &controller}},
		},
		Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"userdata": data},
	}
}

func ownedBootstrap(secret *corev1.Secret, policy *monitoringv2alpha1.WhatapAgent, vmName string) bool {
	owner := metav1.GetControllerOf(secret)
	return owner != nil && owner.UID == policy.UID && owner.Name == policy.Name && owner.Kind == "WhatapAgent" && owner.APIVersion == monitoringv2alpha1.GroupVersion.String() && secret.Immutable != nil && *secret.Immutable && secret.Labels[bootstrapManagedLabel] == "true" && secret.Annotations[bootstrapVMAnnotation] == vmName && len(secret.Data["userdata"]) != 0
}

func (h *KubeVirtAdmissionHandler) ensureBootstrapSecret(ctx context.Context, desired *corev1.Secret, policy *monitoringv2alpha1.WhatapAgent, vmName string) error {
	if err := h.Writer.Create(ctx, desired); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("cannot create bootstrap Secret")
		}
		stored := &corev1.Secret{}
		if err := h.Reader.Get(ctx, client.ObjectKeyFromObject(desired), stored); err != nil {
			return fmt.Errorf("cannot read bootstrap Secret")
		}
		if !ownedBootstrap(stored, policy, vmName) || !bytes.Equal(stored.Data["userdata"], desired.Data["userdata"]) {
			return fmt.Errorf("bootstrap Secret is not owned or its content differs")
		}
	}
	return nil
}

var _ admission.Handler = (*KubeVirtAdmissionHandler)(nil)
