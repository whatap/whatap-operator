package controller

import (
	"strings"
	"testing"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGenerateScrapeConfig_MultiCRD(t *testing.T) {
	cr := &monitoringv2alpha1.WhatapAgent{
		Spec: monitoringv2alpha1.WhatapAgentSpec{
			Features: monitoringv2alpha1.FeaturesSpec{
				OpenAgent: monitoringv2alpha1.OpenAgentSpec{
					Enabled: true,
				},
			},
		},
	}

	podMonitors := &monitoringv2alpha1.WhatapPodMonitorList{
		Items: []monitoringv2alpha1.WhatapPodMonitor{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-mon-1",
					Namespace: "ns-1",
				},
				Spec: monitoringv2alpha1.WhatapPodMonitorSpec{
					Endpoints: []monitoringv2alpha1.OpenAgentEndpoint{
						{Port: "8080", Path: "/metrics"},
					},
				},
			},
		},
	}

	serviceMonitors := &monitoringv2alpha1.WhatapServiceMonitorList{
		Items: []monitoringv2alpha1.WhatapServiceMonitor{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-mon-1",
					Namespace: "ns-2",
				},
				Spec: monitoringv2alpha1.WhatapServiceMonitorSpec{
					Endpoints: []monitoringv2alpha1.OpenAgentEndpoint{
						{Port: "9090", Path: "/metrics"},
					},
				},
			},
		},
	}

	config := generateScrapeConfig(cr, "default", podMonitors, serviceMonitors)

	// Verify PodMonitor
	if !strings.Contains(config, "targetName: ns-1/pod-mon-1") {
		t.Errorf("Expected config to contain 'targetName: ns-1/pod-mon-1', got: \n%s", config)
	}
	if !strings.Contains(config, "type: PodMonitor") {
		t.Errorf("Expected config to contain 'type: PodMonitor'")
	}

	// Verify ServiceMonitor
	if !strings.Contains(config, "targetName: ns-2/svc-mon-1") {
		t.Errorf("Expected config to contain 'targetName: ns-2/svc-mon-1'")
	}
	if !strings.Contains(config, "type: ServiceMonitor") {
		t.Errorf("Expected config to contain 'type: ServiceMonitor'")
	}
}

func TestGenerateScrapeConfig_JobLabel(t *testing.T) {
	cr := &monitoringv2alpha1.WhatapAgent{
		Spec: monitoringv2alpha1.WhatapAgentSpec{
			Features: monitoringv2alpha1.FeaturesSpec{
				OpenAgent: monitoringv2alpha1.OpenAgentSpec{
					Enabled: true,
				},
			},
		},
	}

	podMonitors := &monitoringv2alpha1.WhatapPodMonitorList{
		Items: []monitoringv2alpha1.WhatapPodMonitor{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-job-test",
					Namespace: "default",
				},
				Spec: monitoringv2alpha1.WhatapPodMonitorSpec{
					JobLabel: "app",
					Endpoints: []monitoringv2alpha1.OpenAgentEndpoint{
						{Port: "web"},
					},
				},
			},
		},
	}

	config := generateScrapeConfig(cr, "default", podMonitors, nil)

	// Check if relabelConfigs section exists
	if !strings.Contains(config, "relabelConfigs:") {
		t.Errorf("Expected config to contain 'relabelConfigs:', got: \n%s", config)
	}
	// Check if target_label is 'job'
	if !strings.Contains(config, "target_label: job") {
		t.Errorf("Expected config to contain 'target_label: job', got: \n%s", config)
	}
	// Check if source_labels is present (format might vary)
	if !strings.Contains(config, "source_labels:") {
		t.Errorf("Expected config to contain 'source_labels:', got: \n%s", config)
	}
	// Check if app label is present
	if !strings.Contains(config, "app") {
		t.Errorf("Expected config to contain 'app', got: \n%s", config)
	}
}

func TestGenerateScrapeConfig_GpuMonitoringGroupLabel(t *testing.T) {
	cr := &monitoringv2alpha1.WhatapAgent{
		Spec: monitoringv2alpha1.WhatapAgentSpec{
			Features: monitoringv2alpha1.FeaturesSpec{
				OpenAgent: monitoringv2alpha1.OpenAgentSpec{
					Enabled: true,
				},
				K8sAgent: monitoringv2alpha1.K8sAgentSpec{
					GpuMonitoring: monitoringv2alpha1.GpuMonitoringSpec{
						Enabled:    true,
						GroupLabel: "prjId",
						Interval:   "30s",
					},
				},
			},
		},
	}

	config := generateScrapeConfig(cr, "default", nil, nil)

	if !strings.Contains(config, "targetName: dcgm-exporter-auto") {
		t.Errorf("Expected config to contain GPU auto target, got: \n%s", config)
	}
	if !strings.Contains(config, "whatap_kube_label_gpu_group") {
		t.Errorf("Expected config to contain 'whatap_kube_label_gpu_group', got: \n%s", config)
	}
	if !strings.Contains(config, "prjId") {
		t.Errorf("Expected config to contain group label key 'prjId', got: \n%s", config)
	}
}

func TestGenerateScrapeConfig_GpuMonitoringClusterName(t *testing.T) {
	cr := &monitoringv2alpha1.WhatapAgent{
		Spec: monitoringv2alpha1.WhatapAgentSpec{
			Features: monitoringv2alpha1.FeaturesSpec{
				OpenAgent: monitoringv2alpha1.OpenAgentSpec{
					Enabled: true,
				},
				K8sAgent: monitoringv2alpha1.K8sAgentSpec{
					GpuMonitoring: monitoringv2alpha1.GpuMonitoringSpec{
						Enabled:     true,
						ClusterName: "test-cluster",
					},
				},
			},
		},
	}

	config := generateScrapeConfig(cr, "default", nil, nil)

	if !strings.Contains(config, "targetName: dcgm-exporter-auto") {
		t.Errorf("Expected config to contain GPU auto target")
	}
	if !strings.Contains(config, "target_label: cluster") {
		t.Errorf("Expected config to contain 'target_label: cluster', got: \n%s", config)
	}
	if !strings.Contains(config, "replacement: test-cluster") {
		t.Errorf("Expected config to contain 'replacement: test-cluster', got: \n%s", config)
	}
}
