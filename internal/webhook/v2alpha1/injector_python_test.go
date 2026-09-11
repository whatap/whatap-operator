package v2alpha1

import (
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
)

func TestInjectPythonEnvVars_NativeConnectionAliases(t *testing.T) {
	for _, mode := range []string{"default", "custom"} {
		t.Run(mode, func(t *testing.T) {
			target := monitoringv2alpha1.TargetSpec{Envs: []corev1.EnvVar{
				{Name: EnvWhatapLicense, Value: "test-license"},
				{Name: EnvWhatapHost, Value: "127.0.0.2"},
				{Name: EnvWhatapPort, Value: "16600"},
			}}
			target.Config.Mode = mode
			container := corev1.Container{Name: "app", Env: []corev1.EnvVar{
				{Name: "whatap.server.host", Value: "127.0.0.9"},
				{Name: "whatap.server.port", Value: "26600"},
				{Name: "WHATAP_SERVER_HOST", Value: "127.0.0.1"},
				{Name: "WHATAP_SERVER_HOST", Value: "127.0.0.3"},
				{Name: "WHATAP_SERVER_PORT", Value: "6600"},
			}}
			original := container.DeepCopy()
			got := injectLanguageSpecificEnvVars(container, target, monitoringv2alpha1.WhatapAgent{}, "python", "2.1.2", logr.Discard())
			for name, want := range map[string]string{
				"whatap.server.host": "127.0.0.2", "whatap.server.port": "16600",
				"whatap_server_host": "127.0.0.2", "WHATAP_SERVER_HOST": "127.0.0.2",
				"whatap_server_port": "16600", "WHATAP_SERVER_PORT": "16600",
			} {
				if values := envValues(got, name); !reflect.DeepEqual(values, []string{want}) {
					t.Errorf("%s = %v, want one %q", name, values, want)
				}
			}
			if !reflect.DeepEqual(&container, original) {
				t.Fatal("injection mutated input container")
			}
		})
	}
}
