package v2alpha1

import (
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
)

func TestConnectionEnvVars_PreserveValueFrom(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		from *corev1.EnvVarSource
		get  func(monitoringv2alpha1.WhatapAgent, monitoringv2alpha1.TargetSpec) corev1.EnvVar
	}{
		{name: "license", key: EnvWhatapLicense, from: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "agent-connection"}, Key: "license"}}, get: getWhatapLicenseEnvVar},
		{name: "host", key: EnvWhatapHost, from: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "agent-connection"}, Key: "host"}}, get: getWhatapHostEnvVar},
		{name: "port", key: EnvWhatapPort, from: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "agent-connection"}, Key: "port"}}, get: getWhatapPortEnvVar},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := monitoringv2alpha1.TargetSpec{Envs: []corev1.EnvVar{{Name: tc.key, ValueFrom: tc.from}}}
			got := tc.get(monitoringv2alpha1.WhatapAgent{}, target)
			if got.Name != tc.key || got.Value != "" || !reflect.DeepEqual(got.ValueFrom, tc.from) {
				t.Fatalf("connection reference was lost: %#v", got)
			}
		})
	}
}

func TestInjectPythonEnvVars_NativeAliasesPreserveValueFrom(t *testing.T) {
	hostSource := &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "agent-connection"}, Key: "host"}}
	portSource := &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "agent-connection"}, Key: "port"}}
	target := monitoringv2alpha1.TargetSpec{Envs: []corev1.EnvVar{
		{Name: EnvPythonWhatapHost, ValueFrom: hostSource},
		{Name: EnvPythonWhatapPort, ValueFrom: portSource},
	}}
	got := injectLanguageSpecificEnvVars(corev1.Container{Name: "app"}, target, monitoringv2alpha1.WhatapAgent{}, "python", "2.1.2", logr.Discard())
	for name, source := range map[string]*corev1.EnvVarSource{
		EnvPythonWhatapHost: hostSource, EnvPythonNativeHost: hostSource,
		EnvPythonWhatapPort: portSource, EnvPythonNativePort: portSource,
		EnvJavaWhatapHost: hostSource, EnvJavaWhatapPort: portSource,
	} {
		count := 0
		for _, env := range got {
			if env.Name == name {
				count++
				if env.Value != "" || !reflect.DeepEqual(env.ValueFrom, source) {
					t.Errorf("%s lost its valueFrom reference", name)
				}
			}
		}
		if count != 1 {
			t.Errorf("%s has %d entries, want one", name, count)
		}
	}
}
