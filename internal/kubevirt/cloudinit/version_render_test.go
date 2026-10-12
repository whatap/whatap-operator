package cloudinit

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderPinsDefaultOrSelectedAgentVersion(t *testing.T) {
	for _, version := range []string{"", "2.9.25", "2.9.26"} {
		t.Run("version="+version, func(t *testing.T) {
			config := Config{License: testLicense, Host: "collector.example"}
			encoded, _ := json.Marshal(map[string]string{"AgentVersion": version})
			if err := json.Unmarshal(encoded, &config); err != nil {
				t.Fatal(err)
			}
			output, err := Render(nil, config)
			if err != nil {
				t.Fatal(err)
			}
			script := decodeConfig(t, output)["write_files"].([]interface{})[0].(map[string]interface{})["content"].(string)
			want := version
			if want == "" {
				want = "2.9.25"
			}
			if !strings.Contains(script, "agent_version='"+want+"'") || strings.Contains(script, "__WHATAP_AGENT_VERSION__") {
				t.Fatal("rendered bootstrap does not pin the selected/default release")
			}
		})
	}
}

func TestRenderRejectsUnsafeAgentVersion(t *testing.T) {
	for _, version := range []string{"latest", "2.9.25\n", "2.9.25';id;#", "../2.9.25", strings.Repeat("1", 33) + ".2.3"} {
		config := Config{License: testLicense, Host: "collector.example"}
		encoded, _ := json.Marshal(map[string]string{"AgentVersion": version})
		if err := json.Unmarshal(encoded, &config); err != nil {
			t.Fatal(err)
		}
		output, err := Render(nil, config)
		if err == nil || output != nil {
			t.Fatal("unsafe release reached the executable bootstrap")
		}
		if strings.Contains(err.Error(), version) || strings.Contains(err.Error(), testLicense) {
			t.Fatal("input appeared in the renderer error")
		}
	}
}
