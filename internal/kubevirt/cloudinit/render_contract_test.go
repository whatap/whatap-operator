package cloudinit

import (
	"strings"
	"testing"
)

// whatap.conf values are data, not shell/YAML source. Do not add Java-properties
// escaping to a native server-agent key or collector address.
func TestRenderPreservesLiteralLicenseAndCollectorAddress(t *testing.T) {
	license := "synthetic-base64-license=="
	host := "2001:db8::1/collector.example"
	data, err := Render(nil, Config{License: license, Host: host, Port: 6600})
	if err != nil {
		t.Fatal(err)
	}
	result := decodeConfig(t, data)
	files := result["write_files"].([]interface{})
	content := files[1].(map[string]interface{})["content"].(string)
	if !strings.Contains(content, "license="+license+"\n") || !strings.Contains(content, "whatap.server.host="+host+"\n") {
		t.Fatal("literal connection values were altered")
	}
}

func TestRenderAcceptsEmptyCloudConfigHeader(t *testing.T) {
	data, err := Render([]byte("#cloud-config\n# no guest customization yet\n"), Config{License: testLicense, Host: "collector.example"})
	if err != nil {
		t.Fatal("an empty cloud-config must accept the bootstrap")
	}
	if _, ok := decodeConfig(t, data)["runcmd"]; !ok {
		t.Fatal("bootstrap command missing")
	}
}
