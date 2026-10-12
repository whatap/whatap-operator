package cloudinit

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const testLicense = "synthetic-test-license-not-a-credential"

func decodeConfig(t *testing.T, data []byte) map[string]interface{} {
	t.Helper()
	var value map[string]interface{}
	if err := yaml.Unmarshal(data, &value); err != nil {
		t.Fatal("output is not valid YAML")
	}
	return value
}

func TestRenderPreservesCloudConfigAndAppendsPrivateBootstrap(t *testing.T) {
	input := []byte("#cloud-config\ntimezone: Asia/Seoul\nusers:\n  - name: ubuntu\nssh_authorized_keys: [ssh-ed25519-synthetic]\nnetwork:\n  version: 2\nwrite_files:\n  - path: /etc/motd\n    content: Welcome\nruncmd:\n  - [echo, ready]\n")
	before := append([]byte(nil), input...)
	output, err := Render(input, Config{License: testLicense, Host: "collector.example/192.0.2.1"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input, before) || !bytes.HasPrefix(output, []byte("#cloud-config\n")) {
		t.Fatal("input mutated or output header missing")
	}
	original, result := decodeConfig(t, input), decodeConfig(t, output)
	for _, key := range []string{"timezone", "users", "ssh_authorized_keys", "network"} {
		if !reflect.DeepEqual(original[key], result[key]) {
			t.Errorf("unrelated key changed: %s", key)
		}
	}
	files, ok := result["write_files"].([]interface{})
	if !ok || len(files) != 3 {
		t.Fatal("expected original file plus bootstrap and private configuration")
	}
	if !reflect.DeepEqual(files[0], original["write_files"].([]interface{})[0]) {
		t.Fatal("original file changed")
	}
	script := files[1].(map[string]interface{})
	config := files[2].(map[string]interface{})
	if script["path"] != "/usr/local/sbin/whatap-kubevirt-install.sh" || script["permissions"] != "0700" || script["owner"] != "root:root" {
		t.Fatal("bootstrap is not root-only")
	}
	if config["path"] != "/etc/whatap-kubevirt/whatap.conf" || config["permissions"] != "0600" || config["owner"] != "root:root" {
		t.Fatal("configuration is not root-only")
	}
	content := config["content"].(string)
	if !strings.Contains(content, "license="+testLicense+"\n") || !strings.Contains(content, "whatap.server.port=6600\n") {
		t.Fatal("missing private agent configuration or default port")
	}
	commands, ok := result["runcmd"].([]interface{})
	want := []interface{}{"/bin/bash", "/usr/local/sbin/whatap-kubevirt-install.sh"}
	if !ok || len(commands) != 2 || !reflect.DeepEqual(commands[0], original["runcmd"].([]interface{})[0]) || !reflect.DeepEqual(commands[1], want) {
		t.Fatal("runcmd was not preserved and appended as argv")
	}
	if strings.Contains(script["content"].(string), testLicense) {
		t.Fatal("secret embedded in executable")
	}
}

func TestRenderRejectsUnsafeCloudConfig(t *testing.T) {
	for name, input := range map[string]string{
		"shell":          "#!/bin/bash\necho secret-source-sentinel",
		"mime":           "MIME-Version: 1.0\nsecret-source-sentinel",
		"missing header": "timezone: secret-source-sentinel",
		"scalar":         "#cloud-config\nsecret-source-sentinel",
		"root list":      "#cloud-config\n- secret-source-sentinel",

		"malformed":              "#cloud-config\nsecret-source-sentinel: [",
		"duplicate":              "#cloud-config\ntimezone: UTC\ntimezone: secret-source-sentinel\n",
		"nested duplicate":       "#cloud-config\nusers:\n- name: first\n  name: secret-source-sentinel\n",
		"multiple documents":     "#cloud-config\n{}\n---\nsecret-source-sentinel: 1\n",
		"merge type":             "#cloud-config\nmerge_type: list(replace)\n",
		"merge how":              "#cloud-config\nmerge_how: [secret-source-sentinel]\n",
		"yaml merge":             "#cloud-config\nbase: &base {runcmd: []}\n<<: *base\n",
		"alias":                  "#cloud-config\na: &a [secret-source-sentinel]\nb: *a\n",
		"custom tag":             "#cloud-config\na: !custom secret-source-sentinel\n",
		"invalid typed scalar":   "#cloud-config\na: !!int secret-source-sentinel\n",
		"disable modules":        "#cloud-config\ncloud_final_modules: []\n",
		"files not list":         "#cloud-config\nwrite_files: {}\n",
		"files null":             "#cloud-config\nwrite_files: null\n",
		"file not mapping":       "#cloud-config\nwrite_files: [secret-source-sentinel]\n",
		"file no path":           "#cloud-config\nwrite_files: [{content: secret-source-sentinel}]\n",
		"command not list":       "#cloud-config\nruncmd: secret-source-sentinel\n",
		"invalid command":        "#cloud-config\nruncmd: [{bad: secret-source-sentinel}]\n",
		"empty argv":             "#cloud-config\nruncmd: [[]]\n",
		"script collision":       "#cloud-config\nwrite_files:\n- path: /usr/local/sbin/whatap-kubevirt-install.sh\n",
		"normalized collision":   "#cloud-config\nwrite_files:\n- path: /etc/other/../whatap-kubevirt/whatap.conf\n",
		"config ancestor":        "#cloud-config\nwrite_files:\n- path: /etc/whatap-kubevirt\n",
		"agent config collision": "#cloud-config\nwrite_files:\n- path: /usr/whatap/infra/conf/whatap.conf\n",
		"tar metadata collision": "#cloud-config\nwrite_files:\n- path: /etc/whatap/infra.meta\n",
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() != nil {
					t.Error("unsafe input panicked instead of being rejected")
				}
			}()
			output, err := Render([]byte(input), Config{License: testLicense, Host: "collector.example"})
			if err == nil || output != nil {
				t.Fatal("unsafe input accepted")
			}
			if strings.Contains(err.Error(), "secret-source-sentinel") || strings.Contains(err.Error(), testLicense) {
				t.Fatal("error contains source data")
			}
		})
	}
}

func TestRenderValidatesAndSerializesConnectionSettings(t *testing.T) {
	for _, config := range []Config{
		{Host: "collector.example"},
		{License: " 	", Host: "collector.example"},
		{License: "sentinel\nlicense=bad", Host: "collector.example"},
		{License: "sentinel\x00", Host: "collector.example"},
		{License: testLicense},
		{License: testLicense, Host: "host\nlicense=bad"},
		{License: testLicense, Host: "$(touch /bad)"},
		{License: testLicense, Host: "http://collector.example"},
		{License: testLicense, Host: "collector.example//192.0.2.1"},
		{License: testLicense, Host: "-bad.example"},
		{License: testLicense, Host: "host:6600"},
		{License: testLicense, Host: "collector.example", Port: -1},
		{License: testLicense, Host: "collector.example", Port: 65536},
	} {
		data, err := Render(nil, config)
		if err == nil || data != nil {
			t.Error("invalid settings accepted")
			continue
		}
		if strings.Contains(err.Error(), "sentinel") || strings.Contains(err.Error(), testLicense) {
			t.Fatal("settings leaked into error")
		}
	}
	for _, host := range []string{"collector.example", "192.0.2.1/192.0.2.2", "2001:db8::1/collector.example", "localhost"} {
		data, err := Render(nil, Config{License: `safe\key=foo:bar #!$(literal)`, Host: host, Port: 65535})
		if err != nil {
			t.Fatal("valid settings rejected")
		}
		result := decodeConfig(t, data)
		files := result["write_files"].([]interface{})
		content := files[1].(map[string]interface{})["content"].(string)
		if !strings.Contains(content, `license=safe\key=foo:bar #!$(literal)`+"\n") || !strings.Contains(content, "whatap.server.port=65535\n") {
			t.Fatal("connection values were not preserved as literal data")
		}
	}
}
