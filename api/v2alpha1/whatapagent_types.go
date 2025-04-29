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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// WhatapAgentSpec defines the desired state of WhatapAgent
type WhatapAgentSpec struct {
	License           string       `json:"license"`
	Host              string       `json:"host"`
	Port              string       `json:"port"`
	Features          FeaturesSpec `json:"features"`
	AgentImageVersion string       `json:"agentImageVersion"`
}

type FeaturesSpec struct {
	Apm                  ApmSpec                  `json:"apm"`
	KubernetesMonitoring KubernetesMonitoringSpec `json:"kubernetesMonitoring"`
}
type KubernetesMonitoringSpec struct {
	KubernetesMonitoringNamespace string `json:"KubernetesMonitoringNamespace"`
	MasterAgentEnabled            string `json:"masterAgentEnabled"`
	NodeAgentEnabled              string `json:"nodeAgentEnabled"`
	GpuEnabled                    string `json:"gpuEnabled,omitempty"`       // GPU 모니터링, default=false
	ApiserverEnabled              string `json:"apiserverEnabled,omitempty"` // API Server 모니터링, default=false
	EtcdEnabled                   string `json:"etcdEnabled,omitempty"`      // ETCD 모니터링, default=false
	SchedulerEnabled              string `json:"schedulerEnabled,omitempty"` // Scheduler 모니터링, default=false
	OpenAgentEnabled              string `json:"openAgentEnabled,omitempty"` // openAgent, default=false
}

type ApmSpec struct {
	Instrumentation InstrumentationSpec `json:"instrumentation"`
}

type InstrumentationSpec struct {
	Targets []TargetSpec `json:"targets"`
}

type TargetSpec struct {
	Name    string `json:"name"`
	Enabled string `json:"enabled"` // "true" 문자열
	// +kubebuilder:validation:Enum=java;python;php;dotnet;nodejs;golang
	Language          string            `json:"language"` // ⭐️ Enum 제한
	WhatapApmVersions map[string]string `json:"whatapApmVersions"`
	NamespaceSelector NamespaceSelector `json:"namespaceSelector"`
	PodSelector       PodSelector       `json:"podSelector"`
	Config            ConfigSpec        `json:"config"`
}

type NamespaceSelector struct {
	MatchNames []string `json:"matchNames"`
}

type PodSelector struct {
	MatchLabels map[string]string `json:"matchLabels"`
}

type ConfigSpec struct {
	Mode         string        `json:"mode,omitempty"`         // "default" or "custom"
	ConfigMapRef *ConfigMapRef `json:"configMapRef,omitempty"` // custom 모드일 때만 사용
}

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
