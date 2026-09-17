package v2alpha1

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
)

// Execute the real Node CLI with the assembled environment. The tiny whatap
// package below is a preload fixture, not the real agent or a telemetry E2E.
func TestNodejsEnv_NodeRuntime(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for the real CLI preload probe")
	}
	root := t.TempDir()
	agentModules := filepath.Join(root, "agent_modules")
	appModules := filepath.Join(root, "app_modules")
	files := map[string]string{
		filepath.Join(agentModules, "whatap", "index.js"):   "global.whatapFixtureLoads = (global.whatapFixtureLoads || 0) + 1;",
		filepath.Join(appModules, "appfixture", "index.js"): "module.exports = 'application-path-preserved';",
		filepath.Join(root, "custom preload.js"):            "global.customPreload = true;",
	}
	for name, content := range files {
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	optional := true
	literal := []corev1.EnvVar{
		{Name: EnvNodejsOptions, Value: "--max-old-space-size=512 --require \"" + filepath.Join(root, "custom preload.js") + "\""},
		{Name: EnvNodejsPath, Value: appModules + "::"},
	}
	for _, tc := range []struct {
		name       string
		container  corev1.Container
		sources    map[string]map[string]string
		imageEnv   map[string]string
		instrument bool
		appPath    bool
		custom     bool
	}{
		{name: "unset", instrument: true},
		{name: "literal options and application path", container: corev1.Container{Env: literal}, instrument: true, appPath: true, custom: true},
		{name: "mandatory ConfigMap", container: corev1.Container{Env: []corev1.EnvVar{{Name: EnvNodejsOptions, ValueFrom: nodejsSource("ConfigMap", nil)}, {Name: EnvNodejsPath, Value: appModules}}}, sources: map[string]map[string]string{"selected": {"value": "--max-old-space-size=512"}}, instrument: true, appPath: true},
		{name: "mandatory Secret", container: corev1.Container{Env: []corev1.EnvVar{{Name: EnvNodejsOptions, ValueFrom: nodejsSource("Secret", nil)}}}, sources: map[string]map[string]string{"selected": {"value": "--trace-warnings"}}, instrument: true},
		{name: "optional missing resource", container: corev1.Container{Env: []corev1.EnvVar{{Name: EnvNodejsOptions, ValueFrom: nodejsSource("ConfigMap", &optional)}}}},
		{name: "optional empty", container: corev1.Container{Env: []corev1.EnvVar{{Name: EnvNodejsOptions, ValueFrom: nodejsSource("Secret", &optional)}}}, sources: map[string]map[string]string{"selected": {"value": ""}}},
		{name: "optional missing keeps prior", container: corev1.Container{Env: append(append([]corev1.EnvVar{}, literal...), corev1.EnvVar{Name: EnvNodejsOptions, ValueFrom: nodejsSource("Secret", &optional)})}, instrument: true, appPath: true, custom: true},
		{name: "optional missing preserves image defaults", container: corev1.Container{Env: []corev1.EnvVar{{Name: EnvNodejsOptions, ValueFrom: nodejsSource("Secret", &optional)}}}, imageEnv: map[string]string{EnvNodejsOptions: literal[0].Value, EnvNodejsPath: appModules}, appPath: true, custom: true},
		{name: "unsafe optional path skips preload too", container: corev1.Container{EnvFrom: []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "unread"}}}}, Env: []corev1.EnvVar{{Name: EnvNodejsPath, ValueFrom: nodejsSource("Secret", &optional)}}}},
		{name: "unsafe optional options preserves startup", container: corev1.Container{EnvFrom: []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "unread"}}}}, Env: []corev1.EnvVar{{Name: EnvNodejsOptions, ValueFrom: nodejsSource("Secret", &optional)}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			container := tc.container
			container.Name = "app"
			for attempt := 0; attempt < 2; attempt++ {
				container.Env = nodejsEnv(container, monitoringv2alpha1.TargetSpec{})
				resolved, err := resolveNodejsTestEnv(container.Env, nil, tc.sources)
				if err != nil {
					t.Fatal(err)
				}
				environment := []string{}
				for _, name := range []string{EnvNodejsOptions, EnvNodejsPath} {
					value, exists := resolved[name]
					if !exists {
						value, exists = tc.imageEnv[name]
					}
					if exists {
						environment = append(environment, name+"="+strings.ReplaceAll(value, ValNodejsModules, agentModules))
					}
				}
				assertions := "const assert = require('node:assert/strict');"
				if tc.custom {
					assertions += "assert.equal(global.customPreload, true);"
				}
				if tc.instrument {
					assertions += "assert.equal(global.whatapFixtureLoads, 1);"
				} else {
					assertions += "assert.equal(global.whatapFixtureLoads, undefined);"
				}
				if tc.appPath {
					assertions += "assert.equal(require('appfixture'), 'application-path-preserved');"
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				command := exec.CommandContext(ctx, node, "-e", assertions)
				command.Dir, command.Env = root, environment
				output, err := command.CombinedOutput()
				cancel()
				if err != nil {
					t.Fatalf("Node startup/preload attempt %d: %v\n%s", attempt, err, output)
				}
			}
		})
	}
}
