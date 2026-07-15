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

func TestGenerateScrapeConfig_Authorization(t *testing.T) {
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
				ObjectMeta: metav1.ObjectMeta{Name: "auth-mon", Namespace: "ns-auth"},
				Spec: monitoringv2alpha1.WhatapPodMonitorSpec{
					Endpoints: []monitoringv2alpha1.OpenAgentEndpoint{
						{
							Port: "8080",
							Path: "/metrics",
							Authorization: &monitoringv2alpha1.AuthorizationConfig{
								Type: "Bearer",
								CredentialsSecret: &monitoringv2alpha1.SecretKeySelector{
									Name:      "scrape-token",
									Key:       "token",
									Namespace: "ns-auth",
								},
							},
						},
					},
				},
			},
		},
	}
	serviceMonitors := &monitoringv2alpha1.WhatapServiceMonitorList{}

	config := generateScrapeConfig(cr, "default", podMonitors, serviceMonitors)

	for _, want := range []string{"authorization:", "type: Bearer", "credentialsSecret:", "name: scrape-token", "key: token"} {
		if !strings.Contains(config, want) {
			t.Errorf("Expected config to contain %q, got:\n%s", want, config)
		}
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

func TestGenerateScrapeConfig_RelabelCamelCaseFallback(t *testing.T) {
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
				ObjectMeta: metav1.ObjectMeta{Name: "camel-mon", Namespace: "ns-camel"},
				Spec: monitoringv2alpha1.WhatapPodMonitorSpec{
					// Prometheus Operator style keys only (sourceLabels/targetLabel)
					RelabelConfigs: []monitoringv2alpha1.MetricRelabelConfig{
						{
							SourceLabelsCamel: []string{"__meta_kubernetes_pod_label_team"},
							TargetLabelCamel:  "team",
							Action:            "replace",
						},
					},
					Endpoints: []monitoringv2alpha1.OpenAgentEndpoint{
						{
							Port: "8080",
							// Both styles set: snake_case must win
							MetricRelabelConfigs: []monitoringv2alpha1.MetricRelabelConfig{
								{
									SourceLabels:      []string{"snake_source_wins"},
									SourceLabelsCamel: []string{"camel_source_loses"},
									TargetLabel:       "snake_target_wins",
									TargetLabelCamel:  "camel_target_loses",
									Action:            "replace",
								},
							},
						},
					},
				},
			},
		},
	}

	config := generateScrapeConfig(cr, "default", podMonitors, nil)

	// camelCase-only input must be rendered with snake_case keys
	if !strings.Contains(config, "target_label: team") {
		t.Errorf("Expected camelCase targetLabel to render as 'target_label: team', got: \n%s", config)
	}
	if !strings.Contains(config, "__meta_kubernetes_pod_label_team") {
		t.Errorf("Expected camelCase sourceLabels value to be rendered, got: \n%s", config)
	}
	// snake_case must take precedence when both styles are set
	if !strings.Contains(config, "target_label: snake_target_wins") {
		t.Errorf("Expected snake_case target_label to win, got: \n%s", config)
	}
	if strings.Contains(config, "camel_target_loses") {
		t.Errorf("Expected camelCase targetLabel to be ignored when target_label is set, got: \n%s", config)
	}
	if !strings.Contains(config, "snake_source_wins") {
		t.Errorf("Expected snake_case source_labels to win, got: \n%s", config)
	}
	if strings.Contains(config, "camel_source_loses") {
		t.Errorf("Expected camelCase sourceLabels to be ignored when source_labels is set, got: \n%s", config)
	}
	// rendered scrape config must never contain camelCase relabel keys
	if strings.Contains(config, "sourceLabels:") || strings.Contains(config, "targetLabel:") {
		t.Errorf("Rendered config must only contain snake_case relabel keys, got: \n%s", config)
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
