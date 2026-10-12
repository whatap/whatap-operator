package kubevirt

import (
	"encoding/json"
	"strings"
	"testing"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
)

func TestLicenseSourcesAreExclusive(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		valid  bool
	}{
		{"inline", `"license":"inline-sentinel-not-real=="`, true},
		{"reference", `"licenseSecretRef":{"name":"license","key":"license"}`, true},
		{"both", `"license":"inline-sentinel-not-real==","licenseSecretRef":{"name":"license","key":"license"}`, false},
		{"neither", `"license":""`, false},
		{"empty reference", `"license":"inline-sentinel-not-real==","licenseSecretRef":{}`, false},
		{"blank inline", `"license":"   "`, false},
		{"multiline inline", `"license":"inline-sentinel-not-real\nextra=value"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var target monitoringv2alpha1.KubeVirtTargetSpec
			data := `{"name":"linux","enabled":true,"namespace":"vms","vmSelector":{},"host":"collector.example",` + tc.source + `}`
			if err := json.Unmarshal([]byte(data), &target); err != nil {
				t.Fatal(err)
			}
			err := ValidatePolicy(monitoringv2alpha1.KubeVirtSpec{Enabled: true, Targets: []monitoringv2alpha1.KubeVirtTargetSpec{target}})
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, want %v", err == nil, tc.valid)
			}
			if err != nil && strings.Contains(err.Error(), "inline-sentinel-not-real") {
				t.Fatal("validation error contains the credential")
			}
		})
	}
}
