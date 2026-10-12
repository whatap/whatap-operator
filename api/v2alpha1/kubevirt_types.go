package v2alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KubeVirtSpec is an opt-in, CREATE-only guest bootstrap policy.
// The VM image must already contain enabled cloud-init and a fresh instance state.
// +kubebuilder:object:generate=true
type KubeVirtSpec struct {
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	Targets []KubeVirtTargetSpec `json:"targets,omitempty"`
}

// KubeVirtTargetSpec selects VM metadata, never virt-launcher Pod metadata.
// The VM must additionally carry monitoring.whatap.com/server-agent: "true".
// +kubebuilder:object:generate=true
type KubeVirtTargetSpec struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// Namespace is exact and also scopes every referenced Secret.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
	// VMSelector matches the VirtualMachine's top-level labels.
	VMSelector metav1.LabelSelector `json:"vmSelector"`
	// AgentVersion selects a published official Linux tar release for new VMs only.
	// Changing it does not upgrade agents in existing VMs.
	// +kubebuilder:default="2.9.25"
	// +kubebuilder:validation:Pattern=`^[0-9]+\.[0-9]+\.[0-9]+$`
	// +kubebuilder:validation:MaxLength=32
	// +optional
	AgentVersion string `json:"agentVersion,omitempty"`
	// License is a SERVER monitoring project key supplied directly.
	// It is visible to readers of this CR. Specify exactly one of License or LicenseSecretRef.
	// +optional
	License string `json:"license,omitempty"`
	// LicenseSecretRef alternatively reads the SERVER project key from a Secret in Namespace.
	// Optional Secret/key references are not supported. Never inherits the K8s/APM key.
	// +optional
	LicenseSecretRef *corev1.SecretKeySelector `json:"licenseSecretRef,omitempty"`
	// Host accepts collection server IPs/DNS names, separated by '/'.
	// +kubebuilder:validation:MinLength=1
	Host string `json:"host"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=6600
	// +optional
	Port int32 `json:"port,omitempty"`
}
