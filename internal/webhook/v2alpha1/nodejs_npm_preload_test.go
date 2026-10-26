package v2alpha1

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
)

func TestNpmPreloadIsOptInAndKeepsInitConfigCopy(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		target := monitoringv2alpha1.TargetSpec{Language: "nodejs", Config: monitoringv2alpha1.ConfigSpec{Mode: "custom", ConfigMapRef: &monitoringv2alpha1.ConfigMapRef{Name: "app-config"}}}
		if enabled {
			target.Envs = []corev1.EnvVar{{Name: "WHATAP_SKIP_NPM", Value: "true"}}
		}
		spec := corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Env: []corev1.EnvVar{{Name: "NODE_OPTIONS", Value: "--max-old-space-size=512"}}}}}
		patchPodTemplateSpec(&spec, monitoringv2alpha1.WhatapAgent{}, target, "app", logr.Discard())
		options := ""
		for _, e := range spec.Containers[0].Env {
			if e.Name == "NODE_OPTIONS" {
				options = e.Value
			}
		}
		if !strings.Contains(options, "--max-old-space-size=512") {
			t.Fatal("existing Node option lost")
		}
		if strings.Contains(options, nodejsNpmPreloadPath) != enabled {
			t.Fatalf("unexpected preload: %s", options)
		}
		init := spec.InitContainers[0]
		if !strings.Contains(init.Args[0], "cp /config-volume/whatap.conf /whatap-agent/") {
			t.Fatal("custom config copy lost")
		}
		if strings.Contains(init.Args[0], "printf '%s'") != enabled {
			t.Fatal("unexpected preload file generation")
		}
		if len(spec.Containers[0].Command) != 0 {
			t.Fatal("application command changed")
		}
	}
}

func TestNpmPreloadPreservesInitCommandAndShellContent(t *testing.T) {
	dir := t.TempDir()
	preload := filepath.Join(dir, "preload.cjs")
	sentinel := filepath.Join(dir, "init")
	containers := []corev1.Container{{Command: []string{"sh", "-c"}, Args: []string{"touch " + sentinel}}}
	configureNodejsNpmPreload(containers)
	command := strings.ReplaceAll(containers[0].Args[0], nodejsNpmPreloadPath, preload)
	if out, err := exec.Command("sh", "-c", command).CombinedOutput(); err != nil {
		t.Fatalf("init command failed: %v %s", err, out)
	}
	got, err := os.ReadFile(preload)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != nodejsNpmPreload {
		t.Fatal("preload content changed by shell quoting")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatal("original init command did not run")
	}
}

func TestNpmPreloadRunsRealNpmLifecycleAndDirectNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js required")
	}
	npm, err := exec.LookPath("npm")
	if err != nil {
		t.Skip("npm required")
	}
	root := t.TempDir()
	preload := filepath.Join(root, "preload.cjs")
	files := map[string]string{
		preload: nodejsNpmPreload,
		filepath.Join(root, "node_modules", "whatap", "index.js"): "console.log('WHATAP_FIXTURE:'+require('node:path').basename(process.argv[1]));",
		filepath.Join(root, "package.json"):                       `{"scripts":{"prestart":"node pre.js","start":"node app.js","poststart":"node post.js"}}`,
		filepath.Join(root, "pre.js"):                             "console.log('PRESTART_OK');",
		filepath.Join(root, "post.js"):                            "console.log('POSTSTART_OK');",
		filepath.Join(root, "app.js"):                             "if(!process.env.NODE_OPTIONS.includes('--max-old-space-size=512'))throw Error('options lost'); console.log('APP_OK');",
		filepath.Join(root, "npm-cli.js"):                         "console.log('NAMED_APP_OK');",
	}
	for name, content := range files {
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "NODE_OPTIONS=") && !strings.HasPrefix(e, "NODE_PATH=") {
			env = append(env, e)
		}
	}
	env = append(env, "NODE_OPTIONS=--max-old-space-size=512 -r "+preload, "NODE_PATH="+filepath.Join(root, "node_modules"))
	for _, tc := range []struct {
		name    string
		cmd     string
		args    []string
		markers []string
		loads   int
	}{
		{"npm lifecycle", npm, []string{"run", "start"}, []string{"PRESTART_OK", "APP_OK", "POSTSTART_OK"}, 3},
		{"direct Node", node, []string{"app.js"}, []string{"APP_OK"}, 1},
		{"application named npm-cli", node, []string{"npm-cli.js"}, []string{"NAMED_APP_OK"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(tc.cmd, tc.args...)
			cmd.Dir = root
			cmd.Env = env
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("runtime failed: %v %s", err, out)
			}
			s := string(out)
			if strings.Contains(s, "WHATAP_FIXTURE:npm-cli.js") && tc.name == "npm lifecycle" {
				t.Fatal("npm was instrumented")
			}
			if strings.Count(s, "WHATAP_FIXTURE:") != tc.loads {
				t.Fatalf("unexpected agent loads: %s", s)
			}
			for _, m := range tc.markers {
				if !strings.Contains(s, m) {
					t.Fatalf("missing %s: %s", m, s)
				}
			}
		})
	}
}

func TestNpmPreloadWithDefaultConfig(t *testing.T) {
	target := monitoringv2alpha1.TargetSpec{
		Language: "nodejs",
		Envs:     []corev1.EnvVar{{Name: "WHATAP_SKIP_NPM", Value: "true"}},
	}
	spec := corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}}
	patchPodTemplateSpec(&spec, monitoringv2alpha1.WhatapAgent{}, target, "app", logr.Discard())
	init := spec.InitContainers[0]
	if len(init.Command) != 2 || init.Command[0] != "sh" || init.Command[1] != "-c" {
		t.Fatalf("unexpected init command: %v", init.Command)
	}
	if len(init.Args) != 1 || !strings.HasPrefix(init.Args[0], "if [ -x /init.sh ]; then /init.sh; fi && ") || !strings.Contains(init.Args[0], nodejsNpmPreloadPath) {
		t.Fatalf("agent initialization or preload generation missing: %v", init.Args)
	}
}
