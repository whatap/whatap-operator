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
	// License key for Whatap monitoring
	// +optional
	License string `json:"license,omitempty"`
	// Host address for Whatap server
	// +optional
	Host string `json:"host,omitempty"`
	// Port for Whatap server
	// +optional
	Port     string       `json:"port,omitempty"`
	Features FeaturesSpec `json:"features"`
}

type FeaturesSpec struct {
	Apm       ApmSpec       `json:"apm,omitempty"`
	OpenAgent OpenAgentSpec `json:"openAgent,omitempty"`
	K8sAgent  K8sAgentSpec  `json:"k8sAgent,omitempty"`
}

// OpenAgentSpec defines the openAgent enablement and configuration
type OpenAgentSpec struct {
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`
	// Targets defines the list of targets to scrape metrics from
	// +optional
	Targets []OpenAgentTargetSpec `json:"targets,omitempty"`
	// ImageName defines the name of the OpenAgent image to use
	// +optional
	ImageName string `json:"imageName,omitempty"`
	// ImageVersion defines the version of the OpenAgent image to use
	// +optional
	ImageVersion string `json:"imageVersion,omitempty"`
	// CustomImageFullName allows specifying a full custom image name (including repository and tag)
	// If provided, this takes precedence over ImageName and ImageVersion
	// +optional
	CustomImageFullName string `json:"customImageFullName,omitempty"`
	// Labels to be added to the OpenAgent deployment
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations to be added to the OpenAgent deployment
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// PodLabels to be added to the OpenAgent pod template
	// +optional
	PodLabels map[string]string `json:"podLabels,omitempty"`
	// PodAnnotations to be added to the OpenAgent pod template
	// +optional
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`
	// Tolerations to be added to the OpenAgent pod
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Environment variables to be added to the OpenAgent container
	// +optional
	Envs []corev1.EnvVar `json:"envs,omitempty"`
	// DisableForeground disables foreground mode for the OpenAgent
	// When set to true, the agent will run in background mode
	// +kubebuilder:default=false
	// +optional
	DisableForeground bool `json:"disableForeground,omitempty"`
}

// OpenAgentTargetSpec defines a target for the OpenAgent to scrape metrics from
type OpenAgentTargetSpec struct {
	// TargetName is the name of the target
	TargetName string `json:"targetName"`
	// Type is the type of the target (ServiceMonitor, PodMonitor, or StaticEndpoints)
	// +kubebuilder:validation:Enum=ServiceMonitor;PodMonitor;StaticEndpoints
	Type string `json:"type"`
	// NamespaceSelector selects the namespaces to find the targets in
	NamespaceSelector NamespaceSelector `json:"namespaceSelector,omitempty"`
	// Selector selects the targets to scrape
	Selector PodSelector `json:"selector,omitempty"`
	// Endpoints defines the endpoints to scrape metrics from
	Endpoints []OpenAgentEndpoint `json:"endpoints,omitempty"`

	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`
}

// OpenAgentEndpoint defines an endpoint for the OpenAgent to scrape metrics from
type OpenAgentEndpoint struct {
	// Port is the port to scrape metrics from (for PodMonitor/ServiceMonitor)
	// +optional
	Port string `json:"port,omitempty"`
	// Address is the address to scrape metrics from (for StaticEndpoints)
	// +optional
	Address string `json:"address,omitempty"`
	// Path is the path to scrape metrics from
	// +optional
	Path string `json:"path,omitempty"`
	// Interval is the scrape interval for this endpoint
	// +optional
	Interval string `json:"interval,omitempty"`
	// Scheme is the HTTP scheme to use for scraping (http or https)
	// +optional
	Scheme string `json:"scheme,omitempty"`
	// TLSConfig defines the TLS configuration for the endpoint
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// MetricRelabelConfigs defines the metric relabeling configurations for this endpoint
	// +optional
	MetricRelabelConfigs []MetricRelabelConfig `json:"metricRelabelConfigs,omitempty"`
	// Params defines HTTP URL parameters for the endpoint (similar to Prometheus params)
	// +optional
	Params map[string][]string `json:"params,omitempty"`

	// +kubebuilder:default=false
	// +optional
	AddNodeLabel bool `json:"addNodeLabel,omitempty"`
}

// SecretKeySelector defines a reference to a secret key
type SecretKeySelector struct {
	// Name of the secret
	Name string `json:"name"`
	// Key within the secret
	Key string `json:"key"`
}

// TLSConfig defines the TLS configuration for an endpoint
type TLSConfig struct {
	// InsecureSkipVerify disables target certificate validation
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`

	// CA certificate configuration via Kubernetes Secret
	// +optional
	CASecret *SecretKeySelector `json:"caSecret,omitempty"`

	// Client certificate configuration via Kubernetes Secret
	// +optional
	CertSecret *SecretKeySelector `json:"certSecret,omitempty"`

	// Client private key configuration via Kubernetes Secret
	// +optional
	KeySecret *SecretKeySelector `json:"keySecret,omitempty"`

	// CA certificate file path (alternative to CASecret)
	// +optional
	CAFile string `json:"caFile,omitempty"`

	// Client certificate file path (alternative to CertSecret)
	// +optional
	CertFile string `json:"certFile,omitempty"`

	// Client private key file path (alternative to KeySecret)
	// +optional
	KeyFile string `json:"keyFile,omitempty"`

	// ServerName extension to indicate the name of the server
	// +optional
	ServerName string `json:"serverName,omitempty"`
}

// MetricRelabelConfig defines a metric relabeling configuration
type MetricRelabelConfig struct {
	// SourceLabels is the list of source labels to use in the relabeling
	// +optional
	SourceLabels []string `json:"source_labels,omitempty"`
	// Regex is the regular expression to match against the source labels
	// +optional
	Regex string `json:"regex,omitempty"`
	// TargetLabel is the label to set in the relabeling
	// +optional
	TargetLabel string `json:"target_label,omitempty"`
	// Replacement is the replacement value for the target label
	// +optional
	Replacement string `json:"replacement,omitempty"`
	// Action is the relabeling action to perform
	// +optional
	Action string `json:"action,omitempty"`
}

type K8sAgentSpec struct {
	Namespace string `json:"namespace,omitempty"`
	// AgentImageName defines the name of the agent image to use
	// +optional
	AgentImageName string `json:"agentImageName,omitempty"`
	// AgentImageVersion defines the version of the agent image to use
	// +optional
	AgentImageVersion string `json:"agentImageVersion,omitempty"`
	// CustomAgentImageFullName allows specifying a full custom image name (including repository and tag)
	// If provided, this takes precedence over AgentImageName and AgentImageVersion
	// +optional
	CustomAgentImageFullName string                   `json:"customAgentImageFullName,omitempty"`
	MasterAgent              MasterAgentComponentSpec `json:"masterAgent"`
	NodeAgent                NodeAgentComponentSpec   `json:"nodeAgent"`
	GpuMonitoring            GpuMonitoringSpec        `json:"gpuMonitoring"`
	ApiserverMonitoring      AgentComponentSpec       `json:"apiserverMonitoring,omitempty"`
	EtcdMonitoring           AgentComponentSpec       `json:"etcdMonitoring,omitempty"`
	SchedulerMonitoring      AgentComponentSpec       `json:"schedulerMonitoring,omitempty"`
}

type MasterAgentComponentSpec struct {
	// +kubebuilder:default=false
	Enabled   bool                        `json:"enabled"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	Envs      []corev1.EnvVar             `json:"envs,omitempty"`
	// Tolerations to be added to the MasterAgent pod
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Labels to be added to the MasterAgent deployment
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations to be added to the MasterAgent deployment
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// PodLabels to be added to the MasterAgent pod template
	// +optional
	PodLabels map[string]string `json:"podLabels,omitempty"`
	// PodAnnotations to be added to the MasterAgent pod template
	// +optional
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`
	// MasterAgentContainer defines configuration specific to the whatap-master-agent container
	// +optional
	MasterAgentContainer *ContainerSpec `json:"masterAgentContainer,omitempty"`
}
type NodeAgentComponentSpec struct {
	// +kubebuilder:default=false
	Enabled   bool                        `json:"enabled"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	Envs      []corev1.EnvVar             `json:"envs,omitempty"`
	// Tolerations to be added to the NodeAgent pod
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Labels to be added to the NodeAgent daemonset
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations to be added to the NodeAgent daemonset
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// PodLabels to be added to the NodeAgent pod template
	// +optional
	PodLabels map[string]string `json:"podLabels,omitempty"`
	// PodAnnotations to be added to the NodeAgent pod template
	// +optional
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`
	// NodeAgentContainer defines configuration specific to the whatap-node-agent container
	// +optional
	NodeAgentContainer *ContainerSpec `json:"nodeAgentContainer,omitempty"`
	// NodeHelperContainer defines configuration specific to the whatap-node-helper container
	// +optional
	NodeHelperContainer *ContainerSpec `json:"nodeHelperContainer,omitempty"`
	// Runtime specifies the container runtime (containerd, docker, crio)
	// +kubebuilder:default="containerd"
	// +kubebuilder:validation:Enum=containerd;docker;crio
	// +optional
	Runtime string `json:"runtime,omitempty"`
}

// ContainerSpec defines configuration for a specific container
type ContainerSpec struct {
	// Image defines the container image to use
	// This can be a full image name (including repository and tag)
	// If not provided, the default image will be used
	// +optional
	Image string `json:"image,omitempty"`
	// Resources defines the resource requirements for the container
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// Envs defines environment variables for the container
	// +optional
	Envs []corev1.EnvVar `json:"envs,omitempty"`
}

type AgentComponentSpec struct {
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`
	// CustomImageFullName allows specifying a full custom image name (including repository and tag)
	// If not provided, the default image will be used
	// +optional
	CustomImageFullName string `json:"customImageFullName,omitempty"`
}

// GpuMonitoringSpec defines GPU monitoring specific settings
type GpuMonitoringSpec struct {
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`
	// CustomImageFullName allows specifying a full custom image name (including repository and tag)
	// If not provided, the default image will be used
	// +optional
	CustomImageFullName string `json:"customImageFullName,omitempty"`
	// Service defines service configuration for dcgm-exporter
	// +optional
	Service *GpuMonitoringServiceSpec `json:"service,omitempty"`
}

// GpuMonitoringServiceSpec defines service configuration for GPU monitoring
type GpuMonitoringServiceSpec struct {
	// Enabled controls whether to create a service for dcgm-exporter
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// Type specifies the service type (ClusterIP, NodePort, LoadBalancer)
	// +kubebuilder:default="ClusterIP"
	// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
	// +optional
	Type corev1.ServiceType `json:"type,omitempty"`
	// NodePort specifies the node port when service type is NodePort
	// +optional
	NodePort int32 `json:"nodePort,omitempty"`
	// Port specifies the service port
	// +kubebuilder:default=9400
	// +optional
	Port int32 `json:"port,omitempty"`
}

// ApmSpec defines APM-specific settings
type ApmSpec struct {
	Instrumentation InstrumentationSpec `json:"instrumentation,omitempty"`
}

// InstrumentationSpec holds instrumentation targets
type InstrumentationSpec struct {
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	Targets []TargetSpec `json:"targets,omitempty"`
}

type TargetSpec struct {
	Name              string            `json:"name"`
	Enabled           bool              `json:"enabled"`  // +kubebuilder:default=true
	Language          string            `json:"language"` // +kubebuilder:validation:Enum=java;python;php;dotnet;nodejs;golang
	WhatapApmVersions map[string]string `json:"whatapApmVersions"`
	// CustomImageName allows specifying a full custom image name for the agent
	// If not provided, the default image name format will be used
	// +optional
	CustomImageName string `json:"customImageName,omitempty"`
	// AdditionalArgs allows specifying additional arguments for the agent
	// +optional
	AdditionalArgs    map[string]string `json:"additionalArgs,omitempty"`
	NamespaceSelector NamespaceSelector `json:"namespaceSelector,omitempty"`
	PodSelector       PodSelector       `json:"podSelector,omitempty"`
	Config            ConfigSpec        `json:"config,omitempty"`
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
