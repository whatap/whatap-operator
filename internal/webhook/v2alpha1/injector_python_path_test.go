package v2alpha1

import (
	"reflect"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
)

func TestInjectPythonEnvVars_PythonPathPreservesApplicationPaths(t *testing.T) {
	prefix := ValWhatapHome + ":" + ValPythonBootstrap
	for _, tc := range []struct {
		name string
		envs []corev1.EnvVar
		want string
	}{
		{name: "absent", want: prefix},
		{name: "empty", envs: []corev1.EnvVar{{Name: EnvPythonPath}}, want: prefix},
		{name: "existing", envs: []corev1.EnvVar{{Name: EnvPythonPath, Value: "/app/libs"}}, want: prefix + ":/app/libs"},
		{name: "empty segments", envs: []corev1.EnvVar{{Name: EnvPythonPath, Value: "/app/libs::/app/plugins:"}}, want: prefix + ":/app/libs::/app/plugins:"},
		{name: "already injected", envs: []corev1.EnvVar{{Name: EnvPythonPath, Value: prefix + ":/app/libs:" + ValPythonBootstrap}}, want: prefix + ":/app/libs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			container := corev1.Container{Name: "app", Env: tc.envs}
			original := container.DeepCopy()
			got := injectPythonEnvVars(container, monitoringv2alpha1.TargetSpec{}, monitoringv2alpha1.WhatapAgent{}, "2.1.2", logr.Discard())
			if values := envValues(got, EnvPythonPath); !reflect.DeepEqual(values, []string{tc.want}) {
				t.Fatalf("PYTHONPATH = %v, want one %q", values, tc.want)
			}
			if !reflect.DeepEqual(&container, original) {
				t.Fatal("injection mutated input container")
			}
			container.Env = got
			again := injectPythonEnvVars(container, monitoringv2alpha1.TargetSpec{}, monitoringv2alpha1.WhatapAgent{}, "2.1.2", logr.Discard())
			if values := envValues(again, EnvPythonPath); !reflect.DeepEqual(values, []string{tc.want}) {
				t.Fatalf("reinjection changed PYTHONPATH: %v", values)
			}
		})
	}
}

func TestInjectPythonPath_DuplicateLastWins(t *testing.T) {
	prefix := ValWhatapHome + ":" + ValPythonBootstrap
	for _, tc := range []struct {
		name string
		last corev1.EnvVar
	}{
		{name: "last literal", last: corev1.EnvVar{Name: EnvPythonPath, Value: "/actual-application"}},
		{name: "last self reference", last: corev1.EnvVar{Name: EnvPythonPath, Value: "$(PYTHONPATH):/actual-application"}},
		{name: "last ConfigMap", last: corev1.EnvVar{Name: EnvPythonPath, ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "application-path"}, Key: "path"}}}},
		{name: "last Secret", last: corev1.EnvVar{Name: EnvPythonPath, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "application-path"}, Key: "path"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Kubelet expands against the current map, then updates it per entry:
			// https://github.com/kubernetes/kubernetes/blob/v1.33.0/pkg/kubelet/kubelet_pods.go#L822-L902
			// Earlier declarations must remain raw and in place for these consumers
			// and for a final PYTHONPATH that references its previous value.
			envs := []corev1.EnvVar{
				{Name: "BEFORE", Value: "$(PYTHONPATH)"},
				{Name: EnvPythonPathSource, Value: "keep-source-name"},
				{Name: EnvPythonPath, Value: "/stale"},
				{Name: "BETWEEN", Value: "$(PYTHONPATH)"},
				tc.last,
				{Name: "AFTER", Value: "$(PYTHONPATH)"},
			}
			original := (&corev1.Container{Env: envs}).DeepCopy().Env
			reserved := []corev1.EnvVar{{Name: EnvPythonPathSource + "_1", Value: "keep-reserved-name"}}
			want := append([]corev1.EnvVar{}, envs[:4]...)
			last := tc.last
			if last.ValueFrom != nil {
				source := last
				source.Name = EnvPythonPathSource + "_2"
				want = append(want, source)
				last = corev1.EnvVar{Name: EnvPythonPath, Value: "$(" + source.Name + ")"}
			}
			last.Value = prefix + ":" + last.Value
			want = append(want, last, envs[5])
			got := injectPythonPath(envs, reserved, ValPythonBootstrap, logr.Discard())
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("ordered environment = %#v, want %#v", got, want)
			}
			if !reflect.DeepEqual(envs, original) {
				t.Fatal("injection mutated the input environment")
			}
			if again := injectPythonPath(got, reserved, ValPythonBootstrap, logr.Discard()); !reflect.DeepEqual(again, got) {
				t.Fatalf("reinjection changed environment: %#v", again)
			}
		})
	}
}

func TestInjectPythonPath_OptionalValueFromFallback(t *testing.T) {
	prefix := ValWhatapHome + ":" + ValPythonBootstrap
	optional, mandatory := true, false
	for _, kind := range []string{"ConfigMap", "Secret"} {
		for _, flag := range []struct {
			name  string
			value *bool
		}{{"optional", &optional}, {"mandatory", &mandatory}, {"default", nil}} {
			from := &corev1.EnvVarSource{}
			if kind == "ConfigMap" {
				from.ConfigMapKeyRef = &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "application-path"}, Key: "path", Optional: flag.value}
			} else {
				from.SecretKeyRef = &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "application-path"}, Key: "path", Optional: flag.value}
			}
			earlier := from.DeepCopy()
			if earlier.ConfigMapKeyRef != nil {
				earlier.ConfigMapKeyRef.Name = "earlier"
			} else {
				earlier.SecretKeyRef.Name = "earlier"
			}
			for _, state := range []string{"present", "missing", "empty"} {
				for _, fallback := range []struct {
					name      string
					inherited string
					envs      []corev1.EnvVar
				}{
					{name: "none"},
					{name: "envFrom", inherited: "/inherited/libs"},
					{name: "literal", envs: []corev1.EnvVar{{Name: EnvPythonPath, Value: "/app/libs"}}},
					{name: "interleaved refs", envs: []corev1.EnvVar{
						{Name: EnvPythonPath, ValueFrom: earlier},
						{Name: "EARLIER", Value: "$(PYTHONPATH)"},
						{Name: EnvPythonPath, Value: "$(PYTHONPATH):/plugins"},
					}},
				} {
					t.Run(kind+"/"+flag.name+"/"+state+"/"+fallback.name, func(t *testing.T) {
						// Model the tested kubelet v1.33.0 subset: envFrom first,
						// single-pass expansion, optional missing skips assignment,
						// then last assignment wins. ValueFrom data is not expanded.
						// pkg/kubelet/kubelet_pods.go lines 749-902.
						resolve := func(envs []corev1.EnvVar) (map[string]string, bool) {
							values := map[string]string{"earlier/path": "/earlier/libs"}
							if state != "missing" {
								values["application-path/path"] = ""
								if state == "present" {
									values["application-path/path"] = "/selected/libs"
								}
							}
							resolved := map[string]string{}
							if fallback.inherited != "" {
								resolved[EnvPythonPath] = fallback.inherited
							}
							for _, env := range envs {
								value := env.Value
								if env.ValueFrom == nil {
									pairs := []string{}
									for name, prior := range resolved {
										pairs = append(pairs, "$("+name+")", prior)
									}
									value = strings.NewReplacer(pairs...).Replace(value)
								} else {
									var key string
									var opt *bool
									if ref := env.ValueFrom.ConfigMapKeyRef; ref != nil {
										key, opt = ref.Name+"/"+ref.Key, ref.Optional
									} else if ref := env.ValueFrom.SecretKeyRef; ref != nil {
										key, opt = ref.Name+"/"+ref.Key, ref.Optional
									}
									var ok bool
									value, ok = values[key]
									if !ok {
										if opt != nil && *opt {
											continue
										}
										return nil, false
									}
								}
								resolved[env.Name] = value
							}
							return resolved, true
						}
						envs := append([]corev1.EnvVar{}, fallback.envs...)
						envs = append(envs, corev1.EnvVar{Name: EnvPythonPathSource, Value: "keep"},
							corev1.EnvVar{Name: "BETWEEN", Value: "$(PYTHONPATH)"},
							corev1.EnvVar{Name: EnvPythonPath, ValueFrom: from},
							corev1.EnvVar{Name: "AFTER", Value: "$(PYTHONPATH)"})
						original := (&corev1.Container{Env: envs}).DeepCopy().Env
						reserved := []corev1.EnvVar{{Name: EnvPythonPathSource + "_1"}}
						got := injectPythonPath(envs, reserved, ValPythonBootstrap, logr.Discard())
						before, beforeOK := resolve(envs)
						after, afterOK := resolve(got)
						if beforeOK != afterOK {
							t.Fatal("changed mandatory source failure semantics")
						}
						if beforeOK {
							path, exists := before[EnvPythonPath]
							if !exists {
								path = "$(PYTHONPATH)" // Unresolved references stay literal.
							}
							want := prefix + ":" + path
							if after[EnvPythonPath] != want || after["AFTER"] != want {
								t.Fatalf("effective PYTHONPATH = %q, AFTER = %q, want %q", after[EnvPythonPath], after["AFTER"], want)
							}
							for _, name := range []string{"BETWEEN", "EARLIER", EnvPythonPathSource} {
								if before[name] != after[name] {
									t.Fatalf("changed earlier consumer %s", name)
								}
							}
						}
						sourceIndex := len(got) - 3
						source := got[sourceIndex]
						if source.Name != EnvPythonPathSource+"_2" || source.Value != "" || !reflect.DeepEqual(source.ValueFrom, from) {
							t.Fatalf("source reference/collision not preserved: %#v", source)
						}
						added := 1
						if flag.value != nil && *flag.value {
							added++
							if seed := got[sourceIndex-1]; seed.Name != source.Name || seed.Value != "$(PYTHONPATH)" || seed.ValueFrom != nil {
								t.Fatalf("missing consecutive fallback seed: %#v", seed)
							}
						}
						if len(got) != len(envs)+added || !reflect.DeepEqual(envs, original) {
							t.Fatal("unexpected entry count or input mutation")
						}
						if again := injectPythonPath(got, reserved, ValPythonBootstrap, logr.Discard()); !reflect.DeepEqual(again, got) {
							t.Fatal("reinjection changed environment")
						}
					})
				}
			}
		}
	}
}

func TestInjectPythonEnvVars_PythonPathPreservesValueFrom(t *testing.T) {
	optional := true
	for _, tc := range []struct {
		name string
		from *corev1.EnvVarSource
	}{
		{name: "ConfigMap", from: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "application-path"}, Key: "path"}}},
		{name: "Secret", from: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "application-path"}, Key: "path"}}},
		{name: "optional ConfigMap", from: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "application-path"}, Key: "path", Optional: &optional}}},
		{name: "optional Secret", from: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "application-path"}, Key: "path", Optional: &optional}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			container := corev1.Container{Name: "app", Env: []corev1.EnvVar{
				{Name: "WHATAP_ORIGINAL_PYTHONPATH", Value: "keep-container-value"},
				{Name: EnvPythonPath, ValueFrom: tc.from},
				{Name: "DEPENDENT_PATH", Value: "$(PYTHONPATH)"},
			}}
			target := monitoringv2alpha1.TargetSpec{Envs: []corev1.EnvVar{{Name: "WHATAP_ORIGINAL_PYTHONPATH_1", Value: "keep-target-value"}}}
			original := container.DeepCopy()
			got := injectLanguageSpecificEnvVars(container, target, monitoringv2alpha1.WhatapAgent{}, "python", "2.1.2", logr.Discard())
			sourceIndex, pathIndex, dependentIndex := -1, -1, -1
			for i, env := range got {
				if env.ValueFrom != nil && reflect.DeepEqual(env.ValueFrom, tc.from) && env.Name != EnvPythonPath {
					sourceIndex = i
					if env.Value != "" {
						t.Fatal("ValueFrom source has both value and valueFrom")
					}
				}
				if env.Name == EnvPythonPath {
					pathIndex = i
				}
				if env.Name == "DEPENDENT_PATH" {
					dependentIndex = i
				}
			}
			if sourceIndex < 0 || pathIndex <= sourceIndex || dependentIndex <= pathIndex {
				t.Fatalf("expected source before PYTHONPATH before consumers; indices %d/%d/%d", sourceIndex, pathIndex, dependentIndex)
			}
			want := ValWhatapHome + ":" + ValPythonBootstrap + ":$(" + got[sourceIndex].Name + ")"
			if got[pathIndex].Value != want || got[pathIndex].ValueFrom != nil {
				t.Fatalf("unexpected augmented PYTHONPATH: %#v", got[pathIndex])
			}
			if value, _ := effective(got, "WHATAP_ORIGINAL_PYTHONPATH"); value != "keep-container-value" {
				t.Fatal("overwrote container environment")
			}
			if value, _ := effective(got, "WHATAP_ORIGINAL_PYTHONPATH_1"); value != "keep-target-value" {
				t.Fatal("overwrote target environment")
			}
			if !reflect.DeepEqual(&container, original) {
				t.Fatal("injection mutated input container")
			}
			container.Env = got
			again := injectLanguageSpecificEnvVars(container, target, monitoringv2alpha1.WhatapAgent{}, "python", "2.1.2", logr.Discard())
			// Forced connection keys are re-appended by the existing merge;
			// only path/source/consumer order is part of this regression.
			pathEntries := func(envs []corev1.EnvVar) []corev1.EnvVar {
				var entries []corev1.EnvVar
				for _, env := range envs {
					if env.Name == got[sourceIndex].Name || env.Name == EnvPythonPath || env.Name == "DEPENDENT_PATH" {
						entries = append(entries, env)
					}
				}
				return entries
			}
			if len(again) != len(got) || !reflect.DeepEqual(pathEntries(again), pathEntries(got)) {
				t.Fatal("full reinjection changed path entries or grew environment")
			}
			if value, _ := effective(again, EnvPythonPath); value != want {
				t.Fatalf("reinjection changed source reference: %q", value)
			}
			for _, env := range again {
				if strings.HasPrefix(env.Name, "WHATAP_ORIGINAL_PYTHONPATH") && env.ValueFrom != nil && env.Name != got[sourceIndex].Name {
					t.Fatal("reinjection allocated another source alias")
				}
			}
		})
	}
}
