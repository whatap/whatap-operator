package controller

import (
	"testing"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
)

func TestGetNodeAgentDaemonSetSpec_GpuLabel(t *testing.T) {
	tests := []struct {
		name        string
		gpuEnabled  bool
		expectLabel bool
	}{
		{
			name:        "GPU Monitoring Disabled",
			gpuEnabled:  false,
			expectLabel: false,
		},
		{
			name:        "GPU Monitoring Enabled",
			gpuEnabled:  true,
			expectLabel: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := &monitoringv2alpha1.WhatapAgent{
				Spec: monitoringv2alpha1.WhatapAgentSpec{
					Features: monitoringv2alpha1.FeaturesSpec{
						K8sAgent: monitoringv2alpha1.K8sAgentSpec{
							GpuMonitoring: monitoringv2alpha1.GpuMonitoringSpec{
								Enabled: tt.gpuEnabled,
							},
						},
					},
				},
			}

			res := &corev1.ResourceRequirements{}
			dsSpec := getNodeAgentDaemonSetSpec("test-image", res, cr)

			labels := dsSpec.Template.ObjectMeta.Labels
			val, ok := labels["whatap-gpu"]

			if tt.expectLabel {
				if !ok {
					t.Errorf("Expected label 'whatap-gpu' to be present")
				} else if val != "true" {
					t.Errorf("Expected label 'whatap-gpu' to be 'true', got '%s'", val)
				}
			} else {
				if ok {
					t.Errorf("Expected label 'whatap-gpu' NOT to be present, got '%s'", val)
				}
			}
		})
	}
}

func TestGetOpenAgentArgs(t *testing.T) {
	tests := []struct {
		name     string
		spec     monitoringv2alpha1.OpenAgentSpec
		expected []string
	}{
		{
			name: "foreground mode (default)",
			spec: monitoringv2alpha1.OpenAgentSpec{
				DisableForeground: false,
			},
			expected: nil,
		},
		{
			name: "background mode (daemon)",
			spec: monitoringv2alpha1.OpenAgentSpec{
				DisableForeground: true,
			},
			expected: []string{"-d"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getOpenAgentArgs(tt.spec)

			if len(result) != len(tt.expected) {
				t.Errorf("getOpenAgentArgs() = %v, expected %v", result, tt.expected)
				return
			}

			for i, arg := range result {
				if arg != tt.expected[i] {
					t.Errorf("getOpenAgentArgs() = %v, expected %v", result, tt.expected)
					return
				}
			}
		})
	}
}

func TestGetOpenAgentCommand(t *testing.T) {
	spec := monitoringv2alpha1.OpenAgentSpec{}
	result := getOpenAgentCommand(spec)

	if result != nil {
		t.Errorf("getOpenAgentCommand() = %v, expected nil", result)
	}
}
