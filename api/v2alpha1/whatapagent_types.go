/*
Copyright 2025 whatapK8s.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v2alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// WhatapAgentSpec defines the desired state of WhatapAgent
type WhatapAgentSpec struct {
	License  string       `json:"license"`
	Host     string       `json:"host"`
	Port     string       `json:"port"`
	Features FeaturesSpec `json:"features"`
}

type FeaturesSpec struct {
	Apm       ApmSpec       `json:"apm"`
	OpenAgent OpenAgentSpec `json:"openAgent"`
	K8sAgent  K8sAgentSpec  `json:"k8sAgent"`
}

// OpenAgentSpec defines the openAgent enablement
type OpenAgentSpec struct {
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`
}

type K8sAgentSpec struct {
	Namespace           string                   `json:"namespace,omitempty"`
	AgentImageVersion   string                   `json:"agentImageVersion,omitempty"`
	MasterAgent         MasterAgentComponentSpec `json:"masterAgent"`
	NodeAgent           NodeAgentComponentSpec   `json:"nodeAgent"`
	GpuMonitoring       AgentComponentSpec       `json:"gpuMonitoring"`
	ApiserverMonitoring AgentComponentSpec       `json:"apiserverMonitoring"`
	EtcdMonitoring      AgentComponentSpec       `json:"etcdMonitoring"`
	SchedulerMonitoring AgentComponentSpec       `json:"schedulerMonitoring"`
}

type MasterAgentComponentSpec struct {
	// +kubebuilder:default=false
	Enabled   bool                        `json:"enabled"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	Envs      []corev1.EnvVar             `json:"envs,omitempty"`
}
type NodeAgentComponentSpec struct {
	// +kubebuilder:default=false
	Enabled   bool                        `json:"enabled"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	Envs      []corev1.EnvVar             `json:"envs,omitempty"`
}

type AgentComponentSpec struct {
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`
}

// ApmSpec defines APM-specific settings
type ApmSpec struct {
	Instrumentation InstrumentationSpec `json:"instrumentation"`
}

// InstrumentationSpec holds instrumentation targets
type InstrumentationSpec struct {
	Targets []TargetSpec `json:"targets"`
}

type TargetSpec struct {
	Name              string            `json:"name"`
	Enabled           bool              `json:"enabled"`  // +kubebuilder:default=true
	Language          string            `json:"language"` // +kubebuilder:validation:Enum=java;python;php;dotnet;nodejs;golang
	WhatapApmVersions map[string]string `json:"whatapApmVersions"`
	NamespaceSelector NamespaceSelector `json:"namespaceSelector"`
	PodSelector       PodSelector       `json:"podSelector"`
	Config            ConfigSpec        `json:"config"`
}

// NamespaceSelector matches specific namespaces
type NamespaceSelector struct {
	// matchNames is a list of namespace names to include
	// +optional
	MatchNames []string `json:"matchNames,omitempty"`
	// matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
	// map is equivalent to an element of matchExpressions, whose key field is "key", the
	// operator is "In", and the values array contains only "value". The requirements are ANDed.
	// +optional
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
	// matchExpressions is a list of label selector requirements. The requirements are ANDed.
	// +optional
	MatchExpressions []LabelSelectorRequirement `json:"matchExpressions,omitempty"`
}

// PodSelector matches pods by labels
type PodSelector struct {
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
	// matchExpressions is a list of label selector requirements. The requirements are ANDed.
	// +optional
	MatchExpressions []LabelSelectorRequirement `json:"matchExpressions,omitempty"`
}

// A label selector requirement is a selector that contains values, a key, and an operator that
// relates the key and values.
type LabelSelectorRequirement struct {
	// key is the label key that the selector applies to.
	// +required
	Key string `json:"key"`
	// operator represents a key's relationship to a set of values.
	// Valid operators are In, NotIn, Exists and DoesNotExist.
	// +required
	Operator string `json:"operator"`
	// values is an array of string values. If the operator is In or NotIn,
	// the values array must be non-empty. If the operator is Exists or DoesNotExist,
	// the values array must be empty. This array is replaced during a strategic
	// merge patch.
	// +optional
	Values []string `json:"values,omitempty"`
}

// ConfigSpec holds custom configuration reference
type ConfigSpec struct {
	// Mode can be "default" or "custom"
	// +kubebuilder:validation:Enum=default;custom
	Mode         string        `json:"mode,omitempty"`         // "default" or "custom"
	ConfigMapRef *ConfigMapRef `json:"configMapRef,omitempty"` // custom 모드일 때만 사용
}

// ConfigMapRef identifies a ConfigMap resource
type ConfigMapRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// WhatapAgentStatus defines the observed state of WhatapAgent
type WhatapAgentStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// WhatapAgent is the Schema for the whatapagents API
type WhatapAgent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WhatapAgentSpec   `json:"spec,omitempty"`
	Status WhatapAgentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WhatapAgentList contains a list of WhatapAgent
type WhatapAgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WhatapAgent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WhatapAgent{}, &WhatapAgentList{})
}
