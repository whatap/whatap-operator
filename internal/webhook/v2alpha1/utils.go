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

// findEnvValueByKeys searches for an environment variable by multiple candidate keys.
// Returns the value of the first matching key found.
func findEnvValueByKeys(envs []corev1.EnvVar, keys ...string) (string, bool) {
	for _, key := range keys {
		for _, e := range envs {
			if e.Name == key {
				return e.Value, true
			}
		}
	}
	return "", false
}

func getWhatapLicenseEnvVar(cr monitoringv2alpha1.WhatapAgent, target monitoringv2alpha1.TargetSpec) corev1.EnvVar {
	// target envs에서 license 오버라이드 검색: WHATAP_LICENSE, license (Java/Python whatap.conf 키)
	if val, ok := findEnvValueByKeys(target.Envs, EnvWhatapLicense, EnvJavaLicense); ok {
		return corev1.EnvVar{Name: EnvWhatapLicense, Value: val}
	}
	return corev1.EnvVar{Name: EnvWhatapLicense, Value: config.GetWhatapLicense()}
}

func getWhatapHostEnvVar(cr monitoringv2alpha1.WhatapAgent, target monitoringv2alpha1.TargetSpec) corev1.EnvVar {
	// target envs에서 host 오버라이드 검색: WHATAP_HOST, whatap.server.host, whatap_server_host, WHATAP_SERVER_HOST
	if val, ok := findEnvValueByKeys(target.Envs, EnvWhatapHost, EnvJavaWhatapHost, EnvPythonWhatapHost, EnvNodejsWhatapHost); ok {
		return corev1.EnvVar{Name: EnvWhatapHost, Value: val}
	}
	return corev1.EnvVar{Name: EnvWhatapHost, Value: config.GetWhatapHost()}
}

func getWhatapPortEnvVar(cr monitoringv2alpha1.WhatapAgent, target monitoringv2alpha1.TargetSpec) corev1.EnvVar {
	// target envs에서 port 오버라이드 검색: WHATAP_PORT, whatap.server.port, whatap_server_port, WHATAP_SERVER_PORT
	if val, ok := findEnvValueByKeys(target.Envs, EnvWhatapPort, EnvJavaWhatapPort, EnvPythonWhatapPort, EnvNodejsWhatapPort); ok {
		return corev1.EnvVar{Name: EnvWhatapPort, Value: val}
	}
	return corev1.EnvVar{Name: EnvWhatapPort, Value: config.GetWhatapPort()}
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

// upsertEnvVars overlays overrides onto base BY NAME, forcing the override value to win.
//
// For every override entry, ALL pre-existing entries in base with the same name are
// dropped and the override is appended once. This matters because Kubernetes resolves a
// duplicated env name to its FIRST occurrence: if another mutating webhook / 3rd-party APM
// injected e.g. "whatap.server.host" earlier in container.Env, a plain append (operator
// value last) would be shadowed and the agent would fall back to 127.0.0.1. mergeEnvVars
// (existing-wins) is therefore insufficient for whatap-owned keys — use this instead.
//
// A new slice is returned; base is not mutated. Use only for keys the operator owns and
// must control (license / server host+port / micro / downward-API metadata). Keys where a
// user value should be preserved (e.g. PYTHONPATH, agent paths, app name) must keep
// mergeEnvVars / append semantics.
func upsertEnvVars(base []corev1.EnvVar, overrides []corev1.EnvVar) []corev1.EnvVar {
	overrideNames := make(map[string]struct{}, len(overrides))
	for _, o := range overrides {
		if o.Name != "" {
			overrideNames[o.Name] = struct{}{}
		}
	}
	result := make([]corev1.EnvVar, 0, len(base)+len(overrides))
	for _, e := range base {
		if _, ok := overrideNames[e.Name]; ok {
			continue // drop stale duplicate; operator value re-appended below
		}
		result = append(result, e)
	}
	for _, o := range overrides {
		if o.Name == "" {
			continue
		}
		result = append(result, o)
	}
	return result
}

// combineEnvVars merges incoming env vars onto base with a per-name policy:
//   - if force(name) is true  -> upsert semantics: incoming value REPLACES any pre-existing
//     duplicate in base (operator/CR value wins).
//   - if force(name) is false -> merge semantics: incoming value is added only if the name is
//     not already present (a pre-existing/user value is preserved).
//
// This lets a single pass force whatap-owned connection keys while preserving keys the
// operator only augments (PYTHONPATH/NODE_PATH/NODE_OPTIONS) or that the user owns (app name).
func combineEnvVars(base []corev1.EnvVar, incoming []corev1.EnvVar, force func(name string) bool) []corev1.EnvVar {
	var forced, additive []corev1.EnvVar
	for _, e := range incoming {
		if e.Name != "" && force(e.Name) {
			forced = append(forced, e)
		} else {
			additive = append(additive, e)
		}
	}
	return mergeEnvVars(upsertEnvVars(base, forced), additive)
}

// toNameSet builds a set from the given names, deduping aliases (e.g. EnvJavaLicense and
// EnvPythonLicense both resolve to "license").
func toNameSet(names ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return set
}

// pythonForceEnvNames / nodejsForceEnvNames are the whatap-owned connection/config keys that
// the operator must force onto the pod even if another webhook injected a duplicate earlier.
// App-info keys (app_name/app_process_name/OKIND), search-path keys (PYTHONPATH/NODE_PATH/
// NODE_OPTIONS) and the agent-path keys are intentionally NOT here: those preserve an
// existing/user value (the operator only adds or prepends them).
var (
	pythonForceEnvNames = toNameSet(
		EnvPythonLicense, EnvPythonWhatapHost, EnvPythonWhatapPort,
		EnvWhatapHome, EnvWhatapMicroEnabled,
		EnvNodeIP, EnvNodeName, EnvPodName,
	)
	nodejsForceEnvNames = toNameSet(
		EnvNodejsLicense, EnvNodejsWhatapHost, EnvNodejsWhatapPort,
		EnvWhatapHome, EnvWhatapMicroEnabled,
		EnvNodeIP, EnvNodeName, EnvPodName,
	)
)

// operatorManagedEnvNames is every env name the operator itself produces. User CR target.Envs
// values are allowed to override pre-existing pod duplicates for any OTHER name (e.g. a stale
// whatap.name another webhook left behind), but for these managed names the operator's own
// value is authoritative — the supported override path for them is getWhatap*EnvVar.
var operatorManagedEnvNames = toNameSet(
	EnvWhatapLicense, EnvWhatapHost, EnvWhatapPort,
	EnvNodeIP, EnvNodeName, EnvPodName, EnvWhatapMicroEnabled,
	EnvJavaLicense, EnvJavaWhatapHost, EnvJavaWhatapPort, EnvJavaAgentPath, EnvJavaToolOptions,
	EnvPythonLicense, EnvPythonWhatapHost, EnvPythonWhatapPort, EnvPythonAgentPath, EnvWhatapHome, EnvPythonPath,
	EnvAppName, EnvAppProcessName, EnvOkind,
	EnvNodejsLicense, EnvNodejsWhatapHost, EnvNodejsWhatapPort, EnvNodejsAgentPath, EnvNodejsOptions, EnvNodejsPath,
)

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
