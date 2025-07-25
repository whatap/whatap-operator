package controller

import (
	"testing"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
)

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
