package v2alpha1

import (
	"strings"
	"testing"

	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
)

// envValues returns every value bound to name (in order). Kubernetes applies the FIRST one;
// a correct injection must therefore leave exactly one entry with the operator value.
func envValues(envs []corev1.EnvVar, name string) []string {
	var out []string
	for _, e := range envs {
		if e.Name == name {
			out = append(out, e.Value)
		}
	}
	return out
}

// effective returns the value Kubernetes would apply for name (first occurrence).
func effective(envs []corev1.EnvVar, name string) (string, bool) {
	for _, e := range envs {
		if e.Name == name {
			return e.Value, true
		}
	}
	return "", false
}

func TestUpsertEnvVars_DropsStaleDuplicate(t *testing.T) {
	base := []corev1.EnvVar{
		{Name: "whatap.server.host", Value: "127.0.0.1"}, // injected earlier by a 3rd-party webhook
		{Name: "KEEP", Value: "yes"},
	}
	got := upsertEnvVars(base, []corev1.EnvVar{{Name: "whatap.server.host", Value: "10.0.0.1"}})

	if vals := envValues(got, "whatap.server.host"); len(vals) != 1 || vals[0] != "10.0.0.1" {
		t.Fatalf("expected single host=10.0.0.1, got %v", vals)
	}
	if v, _ := effective(got, "KEEP"); v != "yes" {
		t.Fatalf("unrelated env KEEP was lost: %v", got)
	}
	// base must not be mutated
	if base[0].Value != "127.0.0.1" {
		t.Fatalf("upsertEnvVars mutated base: %v", base)
	}
}

// The core KAZAA-641 regression: a 3rd-party webhook put whatap.server.host=127.0.0.1 at the
// FRONT of container.Env. The operator value must still win (single, correct entry).
func TestInjectJavaEnvVars_OverridesPreInjectedHost(t *testing.T) {
	container := corev1.Container{
		Name: "app",
		Env: []corev1.EnvVar{
			{Name: EnvJavaWhatapHost, Value: "127.0.0.1"}, // stale conflict, first → would normally win
			{Name: EnvJavaToolOptions, Value: "-Dexisting=1"},
		},
	}
	target := monitoringv2alpha1.TargetSpec{
		Envs: []corev1.EnvVar{{Name: EnvWhatapHost, Value: "10.20.30.40"}}, // user/CR override
	}

	got := injectJavaEnvVars(container, target, monitoringv2alpha1.WhatapAgent{}, logr.Discard())

	if vals := envValues(got, EnvJavaWhatapHost); len(vals) != 1 || vals[0] != "10.20.30.40" {
		t.Fatalf("expected single whatap.server.host=10.20.30.40, got %v", vals)
	}
	// JAVA_TOOL_OPTIONS must be preserved and augmented with the -javaagent option, not overridden.
	v, ok := effective(got, EnvJavaToolOptions)
	if !ok || !strings.Contains(v, "-Dexisting=1") || !strings.Contains(v, ValJavaAgentOptionPrefix) {
		t.Fatalf("JAVA_TOOL_OPTIONS not preserved+augmented: %q", v)
	}
}

func TestInjectPythonEnvVars_OverridesPreInjectedHostAndKeepsPythonPath(t *testing.T) {
	container := corev1.Container{
		Name: "app",
		Env: []corev1.EnvVar{
			{Name: EnvPythonWhatapHost, Value: "127.0.0.1"}, // stale conflict
			{Name: EnvPythonPath, Value: "/app/libs"},       // user value must be preserved
		},
	}
	target := monitoringv2alpha1.TargetSpec{
		Envs: []corev1.EnvVar{{Name: EnvWhatapHost, Value: "10.20.30.40"}},
	}

	got := injectPythonEnvVars(container, target, monitoringv2alpha1.WhatapAgent{}, "latest", logr.Discard())

	if vals := envValues(got, EnvPythonWhatapHost); len(vals) != 1 || vals[0] != "10.20.30.40" {
		t.Fatalf("expected single whatap_server_host=10.20.30.40, got %v", vals)
	}
	if v, _ := effective(got, EnvPythonPath); v != "/app/libs" {
		t.Fatalf("user PYTHONPATH not preserved, got %q", v)
	}
}

func TestInjectNodejsEnvVars_OverridesPreInjectedHostAndKeepsNodeOptions(t *testing.T) {
	container := corev1.Container{
		Name: "app",
		Env: []corev1.EnvVar{
			{Name: EnvNodejsWhatapHost, Value: "127.0.0.1"},
			{Name: EnvNodejsOptions, Value: "--max-old-space-size=512"}, // user value preserved
		},
	}
	target := monitoringv2alpha1.TargetSpec{
		Envs: []corev1.EnvVar{{Name: EnvWhatapHost, Value: "10.20.30.40"}},
	}

	got := injectNodejsEnvVars(container, target, monitoringv2alpha1.WhatapAgent{}, "latest", logr.Discard())

	if vals := envValues(got, EnvNodejsWhatapHost); len(vals) != 1 || vals[0] != "10.20.30.40" {
		t.Fatalf("expected single WHATAP_SERVER_HOST=10.20.30.40, got %v", vals)
	}
	if v, _ := effective(got, EnvNodejsOptions); v != "--max-old-space-size=512" {
		t.Fatalf("user NODE_OPTIONS not preserved, got %q", v)
	}
}

// End-to-end at the assembly point: a stale whatap.name left by another webhook must be
// overridden by the user's CR target.Envs value, and the operator host must still win.
func TestInjectLanguageSpecific_UserEnvOverridesStaleAndHostWins(t *testing.T) {
	container := corev1.Container{
		Name: "app",
		Env: []corev1.EnvVar{
			{Name: "whatap.name", Value: "wrong-auto-name"}, // 3rd-party stale value
			{Name: EnvJavaWhatapHost, Value: "127.0.0.1"},   // 3rd-party stale value
		},
	}
	target := monitoringv2alpha1.TargetSpec{
		Envs: []corev1.EnvVar{
			{Name: "whatap.name", Value: "my-service"}, // user CR override (operator does not manage this key)
			{Name: EnvWhatapHost, Value: "10.20.30.40"},
		},
	}

	got := injectLanguageSpecificEnvVars(container, target, monitoringv2alpha1.WhatapAgent{}, "java", "latest", logr.Discard())

	if vals := envValues(got, "whatap.name"); len(vals) != 1 || vals[0] != "my-service" {
		t.Fatalf("expected single whatap.name=my-service, got %v", vals)
	}
	if vals := envValues(got, EnvJavaWhatapHost); len(vals) != 1 || vals[0] != "10.20.30.40" {
		t.Fatalf("expected single whatap.server.host=10.20.30.40, got %v", vals)
	}
}
