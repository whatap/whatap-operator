package v2alpha1

import (
	"fmt"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	"github.com/whatap/whatap-operator/internal/config"
	corev1 "k8s.io/api/core/v1"
)

// Helper functions for pointer types
func boolPtr(b bool) *bool    { return &b }
func int64Ptr(i int64) *int64 { return &i }

func getWhatapLicenseEnvVar(cr monitoringv2alpha1.WhatapAgent) corev1.EnvVar {
	license := config.GetWhatapLicense()
	return corev1.EnvVar{Name: EnvWhatapLicense, Value: license}
}

func getWhatapHostEnvVar(cr monitoringv2alpha1.WhatapAgent) corev1.EnvVar {
	host := config.GetWhatapHost()
	return corev1.EnvVar{Name: EnvWhatapHost, Value: host}
}

func getWhatapPortEnvVar(cr monitoringv2alpha1.WhatapAgent) corev1.EnvVar {
	port := config.GetWhatapPort()
	return corev1.EnvVar{Name: EnvWhatapPort, Value: port}
}

func appendIfNotExists(volumes []corev1.Volume, newVol corev1.Volume) []corev1.Volume {
	for _, v := range volumes {
		if v.Name == newVol.Name {
			return volumes
		}
	}
	return append(volumes, newVol)
}

// mergeEnvVars appends extras into base without overriding existing names
func mergeEnvVars(base []corev1.EnvVar, extras []corev1.EnvVar) []corev1.EnvVar {
	existing := make(map[string]struct{}, len(base))
	for _, e := range base {
		if e.Name != "" {
			existing[e.Name] = struct{}{}
		}
	}
	for _, e := range extras {
		if e.Name == "" {
			continue
		}
		if _, ok := existing[e.Name]; ok {
			continue
		}
		base = append(base, e)
		existing[e.Name] = struct{}{}
	}
	return base
}

// matchesSelector checks if the given labels match the selector
func matchesSelector(labels map[string]string, selector monitoringv2alpha1.PodSelector) bool {
	// Check matchLabels
	if !hasLabels(labels, selector.MatchLabels) {
		return false
	}

	// Check matchExpressions
	return matchesLabelExpressions(labels, selector.MatchExpressions)
}

func hasLabels(labels map[string]string, selector map[string]string) bool {
	for key, val := range selector {
		if v, ok := labels[key]; !ok || v != val {
			return false
		}
	}
	return true
}

func matchesLabelExpressions(labels map[string]string, expressions []monitoringv2alpha1.LabelSelectorRequirement) bool {
	for _, req := range expressions {
		if !matchesLabelExpression(labels, req) {
			return false
		}
	}
	return true
}

func matchesLabelExpression(labels map[string]string, req monitoringv2alpha1.LabelSelectorRequirement) bool {
	switch req.Operator {
	case "In":
		return matchesInOperator(labels, req)
	case "NotIn":
		return matchesNotInOperator(labels, req)
	case "Exists":
		return matchesExistsOperator(labels, req)
	case "DoesNotExist":
		return matchesDoesNotExistOperator(labels, req)
	default:
		return false
	}
}

// matchesInOperator checks if label value is in the specified values
func matchesInOperator(labels map[string]string, req monitoringv2alpha1.LabelSelectorRequirement) bool {
	value, exists := labels[req.Key]
	if !exists {
		return false
	}
	for _, v := range req.Values {
		if value == v {
			return true
		}
	}
	return false
}

// matchesNotInOperator checks if label value is not in the specified values
func matchesNotInOperator(labels map[string]string, req monitoringv2alpha1.LabelSelectorRequirement) bool {
	value, exists := labels[req.Key]
	if !exists {
		return true
	}
	for _, v := range req.Values {
		if value == v {
			return false
		}
	}
	return true
}

// matchesExistsOperator checks if label exists
func matchesExistsOperator(labels map[string]string, req monitoringv2alpha1.LabelSelectorRequirement) bool {
	_, exists := labels[req.Key]
	return exists
}

// matchesDoesNotExistOperator checks if label does not exist
func matchesDoesNotExistOperator(labels map[string]string, req monitoringv2alpha1.LabelSelectorRequirement) bool {
	_, exists := labels[req.Key]
	return !exists
}

// matchesNamespaceSelector checks if the given namespace matches the selector
func matchesNamespaceSelector(namespaceName string, namespaceLabels map[string]string, selector monitoringv2alpha1.NamespaceSelector) bool {
	// Check matchNames
	if !matchesNamespaceNames(namespaceName, selector.MatchNames) {
		return false
	}

	// Check matchLabels
	if !hasLabels(namespaceLabels, selector.MatchLabels) {
		return false
	}

	// Check matchExpressions
	return matchesLabelExpressions(namespaceLabels, selector.MatchExpressions)
}

// matchesNamespaceNames checks if namespace name matches any of the specified names
func matchesNamespaceNames(namespaceName string, matchNames []string) bool {
	if len(matchNames) == 0 {
		return true
	}

	for _, name := range matchNames {
		if namespaceName == name {
			return true
		}
	}
	return false
}

// getAgentImage returns the image name to use for the agent
func getAgentImage(target monitoringv2alpha1.TargetSpec, lang, version string) string {
	// Prefer new CustomImageFullName if provided
	if target.CustomImageFullName != "" {
		return target.CustomImageFullName
	}
	// Fallback to deprecated CustomImageName for backward compatibility
	if target.CustomImageName != "" {
		return target.CustomImageName
	}
	// Default image format
	return fmt.Sprintf("public.ecr.aws/whatap/apm-init-%s:%s", lang, version)
}
