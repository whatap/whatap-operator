package v2alpha1

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
)

func nodejsEnv(container corev1.Container, target monitoringv2alpha1.TargetSpec) []corev1.EnvVar {
	return injectLanguageSpecificEnvVars(container, target, monitoringv2alpha1.WhatapAgent{}, "nodejs", "latest", logr.Discard())
}

// Ignore the pre-existing relocation of forced connection keys on reinjection.
func nodejsEntries(envs []corev1.EnvVar) []corev1.EnvVar {
	var out []corev1.EnvVar
	for _, env := range envs {
		if env.Name == EnvNodejsPath || env.Name == EnvNodejsOptions ||
			strings.HasPrefix(env.Name, "WHATAP_ORIGINAL_NODE_") ||
			env.Name == "BEFORE" || env.Name == "BETWEEN" || env.Name == "AFTER" {
			out = append(out, env)
		}
	}
	return out
}

func TestNodejsEnv_OrderedLiteralDeclarations(t *testing.T) {
	for _, tc := range []struct{ name, prior, last, prefix string }{
		{EnvNodejsOptions, "--trace-warnings", "--max-old-space-size=512", ValNodejsRequire + " "},
		{EnvNodejsOptions, "--trace-warnings", "$(NODE_OPTIONS) --max-old-space-size=512", ValNodejsRequire + " "},
		{EnvNodejsPath, "/first", "/last::", ValNodejsModules + ":"},
		{EnvNodejsPath, "/first", "$(NODE_PATH):/last::", ValNodejsModules + ":"},
	} {
		t.Run(tc.name+"/"+tc.last, func(t *testing.T) {
			envs := []corev1.EnvVar{
				{Name: "BEFORE", Value: "$(" + tc.name + ")"},
				{Name: tc.name, Value: tc.prior},
				{Name: "BETWEEN", Value: "$(" + tc.name + ")"},
				{Name: tc.name, Value: tc.last},
				{Name: "AFTER", Value: "$(" + tc.name + ")"},
			}
			container := corev1.Container{Name: "app", Env: envs}
			original := container.DeepCopy()
			got := nodejsEnv(container, monitoringv2alpha1.TargetSpec{})
			want := append([]corev1.EnvVar{}, envs...)
			want[3].Value = tc.prefix + tc.last
			if !reflect.DeepEqual(got[:len(want)], want) {
				t.Fatalf("ordered declarations = %#v, want %#v", got[:len(want)], want)
			}
			if !reflect.DeepEqual(&container, original) {
				t.Fatal("input mutated")
			}
			container.Env = got
			again := nodejsEnv(container, monitoringv2alpha1.TargetSpec{})
			if len(again) != len(got) || !reflect.DeepEqual(nodejsEntries(again), nodejsEntries(got)) {
				t.Fatal("reinjection changed ordered declarations")
			}
		})
	}
}

func TestNodejsEnv_ValueFrom(t *testing.T) {
	mandatory := false
	for _, name := range []string{EnvNodejsPath, EnvNodejsOptions} {
		for _, kind := range []string{"ConfigMap", "Secret"} {
			for _, flag := range []struct {
				label string
				value *bool
			}{{"default", nil}, {"mandatory", &mandatory}} {
				t.Run(name+"/"+kind+"/"+flag.label, func(t *testing.T) {
					from := nodejsSource(kind, flag.value)
					base := "WHATAP_ORIGINAL_" + name
					prior := corev1.EnvVar{Name: name, ValueFrom: nodejsSource("ConfigMap", nil)}
					prior.ValueFrom.ConfigMapKeyRef.Name = "earlier"
					container := corev1.Container{Name: "app", Env: []corev1.EnvVar{
						{Name: base, Value: "keep-container"}, prior,
						{Name: "BETWEEN", Value: "$(" + name + ")"},
						{Name: name, ValueFrom: from},
						{Name: "AFTER", Value: "$(" + name + ")"},
					}}
					target := monitoringv2alpha1.TargetSpec{Envs: []corev1.EnvVar{
						{Name: base + "_1", Value: "keep-target"},
						{Name: name, Value: "must-not-override-container"},
					}}
					original, originalTarget := container.DeepCopy(), target.DeepCopy()
					got := nodejsEnv(container, target)
					alias := corev1.EnvVar{Name: base + "_2", ValueFrom: from}
					prefix := ValNodejsModules + ":"
					if name == EnvNodejsOptions {
						prefix = ValNodejsRequire + " "
					}
					want := append([]corev1.EnvVar{}, container.Env[:3]...)
					want = append(want, alias, corev1.EnvVar{Name: name, Value: prefix + "$(" + alias.Name + ")"}, container.Env[4])
					if !reflect.DeepEqual(got[:len(want)], want) {
						t.Fatal("source alias must preserve source and earlier declarations, precede final value and its consumer, and avoid container/target names")
					}
					if !reflect.DeepEqual(&container, original) || !reflect.DeepEqual(&target, originalTarget) {
						t.Fatal("input mutated")
					}
					if values := envValues(got, base+"_1"); !reflect.DeepEqual(values, []string{"keep-target"}) {
						t.Fatal("target alias collision")
					}
					container.Env = got
					again := nodejsEnv(container, target)
					if len(again) != len(got) || !reflect.DeepEqual(nodejsEntries(again), nodejsEntries(got)) {
						t.Fatal("reinjection changed source aliases or ordered declarations")
					}
				})
			}
		}
	}
}

func nodejsSource(kind string, optional *bool) *corev1.EnvVarSource {
	if kind == "ConfigMap" {
		return &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "selected"}, Key: "value", Optional: optional}}
	}
	return &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "selected"}, Key: "value", Optional: optional}}
}

func TestNodejsEnv_OptionalSourceFallback(t *testing.T) {
	optional := true
	for _, name := range []string{EnvNodejsOptions, EnvNodejsPath} {
		for _, kind := range []string{"ConfigMap", "Secret"} {
			for _, prior := range []string{"none", "literal", "mandatory source", "optional source", "self reference"} {
				for _, hasEnvFrom := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/envFrom=%t", name, kind, prior, hasEnvFrom), func(t *testing.T) {
						value := "/application::"
						prefix := ValNodejsModules + ":"
						if name == EnvNodejsOptions {
							value, prefix = "--trace-warnings", ValNodejsRequire+" "
						}
						base := "WHATAP_ORIGINAL_" + name
						container := corev1.Container{Name: "app", Env: []corev1.EnvVar{{Name: base, Value: "keep-container"}}}
						if hasEnvFrom {
							container.EnvFrom = []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "unread-inherited"}}}}
						}
						if prior == "literal" || prior == "self reference" {
							container.Env = append(container.Env, corev1.EnvVar{Name: name, Value: value})
						} else if prior != "none" {
							var flag *bool
							if prior == "optional source" {
								flag = &optional
							}
							source := nodejsSource("ConfigMap", flag)
							source.ConfigMapKeyRef.Name = "earlier"
							container.Env = append(container.Env, corev1.EnvVar{Name: name, ValueFrom: source})
						}
						if prior == "self reference" {
							container.Env = append(container.Env, corev1.EnvVar{Name: name, Value: "$(" + name + ")"})
						}
						container.Env = append(container.Env, corev1.EnvVar{Name: "BETWEEN", Value: "$(" + name + ")"},
							corev1.EnvVar{Name: name, ValueFrom: nodejsSource(kind, &optional)},
							corev1.EnvVar{Name: "AFTER", Value: "$(" + name + ")"})
						target := monitoringv2alpha1.TargetSpec{Envs: []corev1.EnvVar{{Name: base + "_1", Value: "keep-target"}}}
						original, originalTarget := container.DeepCopy(), target.DeepCopy()
						var logs strings.Builder
						logger := funcr.New(func(_, line string) { logs.WriteString(line) }, funcr.Options{})
						got := injectLanguageSpecificEnvVars(container, target, monitoringv2alpha1.WhatapAgent{}, "nodejs", "latest", logger)
						// Unknown fallback: do not emit unresolved references or replace
						// image ENV defaults when an optional key is absent.
						skip := prior == "optional source" || prior == "none"
						if skip && !strings.Contains(logs.String(), "Skipping Node.js environment augmentation") {
							t.Error("unsupported optional fallback must be diagnosed without claiming successful augmentation")
						}
						for _, state := range []string{"present", "empty", "missing key", "missing resource"} {
							for _, inherited := range []map[string]string{{}, {name: value}} {
								if !hasEnvFrom && len(inherited) > 0 {
									continue
								}
								for _, earlierPresent := range []bool{false, true} {
									sources := map[string]map[string]string{}
									if prior == "mandatory source" || earlierPresent {
										sources["earlier"] = map[string]string{"value": value}
									}
									if state != "missing resource" {
										sources["selected"] = map[string]string{}
									}
									if state == "present" {
										sources["selected"]["value"] = value
									}
									if state == "empty" {
										sources["selected"]["value"] = ""
									}
									before, beforeErr := resolveNodejsTestEnv(container.Env, inherited, sources)
									after, afterErr := resolveNodejsTestEnv(got, inherited, sources)
									if beforeErr != nil || afterErr != nil {
										t.Fatalf("fixture resolution error: %v / %v", beforeErr, afterErr)
									}
									want := before[name]
									if !skip {
										want = prefix + want
									}
									if after[name] != want || (!skip && after["AFTER"] != want) || after["BETWEEN"] != before["BETWEEN"] {
										t.Fatalf("%s: effective %s=%q, want %q; before=%q after=%q", state, name, after[name], want, before["BETWEEN"], after["BETWEEN"])
									}
									if skip {
										_, wasSet := before[name]
										_, isSet := after[name]
										if wasSet != isSet || after["AFTER"] != before["AFTER"] {
											t.Fatal("unsupported fallback changed presence/consumer")
										}
									}
								}
							}
						}
						if !reflect.DeepEqual(&container, original) || !reflect.DeepEqual(&target, originalTarget) {
							t.Fatal("input mutated")
						}
						container.Env = got
						again := nodejsEnv(container, target)
						if len(again) != len(got) || !reflect.DeepEqual(nodejsEntries(again), nodejsEntries(got)) {
							t.Fatal("optional-source reinjection grew or changed environment")
						}
						if strings.Contains(logs.String(), "unread-inherited") || strings.Contains(logs.String(), "keep-container") || strings.Contains(logs.String(), value) {
							t.Fatal("logged user source/value")
						}
					})
				}
			}
		}
	}
}

func TestNodejsEnv_OptionalOptionsRollsBackPathAlias(t *testing.T) {
	optional := true
	for _, kind := range []string{"ConfigMap", "Secret"} {
		t.Run(kind, func(t *testing.T) {
			container := corev1.Container{Name: "app", Env: []corev1.EnvVar{
				{Name: EnvNodejsPath, ValueFrom: nodejsSource("ConfigMap", nil)},
				{Name: EnvNodejsOptions, ValueFrom: nodejsSource(kind, &optional)},
			}}
			got := nodejsEnv(container, monitoringv2alpha1.TargetSpec{})
			var nodeEnvs []corev1.EnvVar
			for _, env := range got {
				if env.Name == EnvNodejsPath || env.Name == EnvNodejsOptions || strings.HasPrefix(env.Name, "WHATAP_ORIGINAL_") {
					nodeEnvs = append(nodeEnvs, env)
				}
			}
			if !reflect.DeepEqual(nodeEnvs, container.Env) {
				t.Fatal("unsafe optional options must roll back both Node variables and the previously allocated path alias")
			}
		})
	}
}

// Model only the kubelet v1.33.0 subset used here: envFrom first, one-pass
// expansion (including $$ escapes), optional missing skips, last assignment wins.
// ValueFrom contents are opaque and are never expanded. This is not a kubelet E2E.
// https://github.com/kubernetes/kubernetes/blob/v1.33.0/pkg/kubelet/kubelet_pods.go#L749-L902
func resolveNodejsTestEnv(envs []corev1.EnvVar, inherited map[string]string, sources map[string]map[string]string) (map[string]string, error) {
	values := map[string]string{}
	for k, v := range inherited {
		values[k] = v
	}
	expression := regexp.MustCompile(`\$\$|\$\([^)]+\)`)
	for _, env := range envs {
		value := env.Value
		if from := env.ValueFrom; from != nil {
			var source, key string
			var optional *bool
			if ref := from.ConfigMapKeyRef; ref != nil {
				source, key, optional = ref.Name, ref.Key, ref.Optional
			} else if ref := from.SecretKeyRef; ref != nil {
				source, key, optional = ref.Name, ref.Key, ref.Optional
			} else {
				continue
			} // Unrelated downward-API metadata.
			var ok bool
			value, ok = sources[source][key]
			if !ok {
				if optional != nil && *optional {
					continue
				}
				return nil, fmt.Errorf("missing mandatory test source")
			}
		} else {
			value = expression.ReplaceAllStringFunc(value, func(token string) string {
				if token == "$$" {
					return "$"
				}
				if prior, ok := values[token[2:len(token)-1]]; ok {
					return prior
				}
				return token
			})
		}
		values[env.Name] = value
	}
	return values, nil
}

func TestNodejsEnv_LiteralComposition(t *testing.T) {
	for _, tc := range []struct {
		name string
		envs []corev1.EnvVar
		path string
		opts string
	}{
		{name: "unset control", path: ValNodejsModules, opts: ValNodejsRequire},
		{name: "empty", envs: []corev1.EnvVar{{Name: EnvNodejsPath}, {Name: EnvNodejsOptions}}, path: ValNodejsModules + ":", opts: ValNodejsRequire},
		{name: "literal", envs: []corev1.EnvVar{{Name: EnvNodejsPath, Value: "/app::/plugins:"}, {Name: EnvNodejsOptions, Value: "--max-old-space-size=512 --require \"/app/custom preload.js\""}}, path: ValNodejsModules + ":/app::/plugins:", opts: ValNodejsRequire + " --max-old-space-size=512 --require \"/app/custom preload.js\""},
		{name: "already injected", envs: []corev1.EnvVar{{Name: EnvNodejsPath, Value: ValNodejsModules + ":/app:" + ValNodejsModules + ":"}, {Name: EnvNodejsOptions, Value: ValNodejsRequire + " --trace-warnings"}}, path: ValNodejsModules + ":/app:", opts: ValNodejsRequire + " --trace-warnings"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			container := corev1.Container{Name: "app", Env: tc.envs}
			original := container.DeepCopy()
			got := nodejsEnv(container, monitoringv2alpha1.TargetSpec{})
			for name, want := range map[string]string{EnvNodejsPath: tc.path, EnvNodejsOptions: tc.opts} {
				if values := envValues(got, name); !reflect.DeepEqual(values, []string{want}) {
					t.Errorf("%s = %q, want one %q", name, values, want)
				}
			}
			if !reflect.DeepEqual(&container, original) {
				t.Fatal("input container mutated")
			}
			container.Env = got
			again := nodejsEnv(container, monitoringv2alpha1.TargetSpec{})
			if len(again) != len(got) || !reflect.DeepEqual(nodejsEntries(again), nodejsEntries(got)) {
				t.Fatal("reinjection changed Node environment or grew entries")
			}
		})
	}
}
