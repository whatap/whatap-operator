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

// WhatapStaticEndpointSpec defines the desired state of WhatapStaticEndpoint.
// StaticEndpoints target fixed addresses (host:port) directly and do not
// require the Kubernetes API to discover pods or services, so no selector
// is needed.
type WhatapStaticEndpointSpec struct {
	// Endpoints defines the static endpoints (address:port) to scrape metrics from
	Endpoints []OpenAgentEndpoint `json:"endpoints"`

	// RelabelConfigs defines the relabeling configurations for this target
	// +optional
	RelabelConfigs []MetricRelabelConfig `json:"relabelConfigs,omitempty"`

	// JobLabel is the label to use as the job name. If not specified, the CR name is used.
	// +optional
	JobLabel string `json:"jobLabel,omitempty"`
}

// WhatapStaticEndpointStatus defines the observed state of WhatapStaticEndpoint
type WhatapStaticEndpointStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// WhatapStaticEndpoint is the Schema for the whatapstaticendpoints API
type WhatapStaticEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WhatapStaticEndpointSpec   `json:"spec,omitempty"`
	Status WhatapStaticEndpointStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// WhatapStaticEndpointList contains a list of WhatapStaticEndpoint
type WhatapStaticEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WhatapStaticEndpoint `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WhatapStaticEndpoint{}, &WhatapStaticEndpointList{})
}
