package v2alpha1

import (
	"os"
	"regexp"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func TestKubeVirtAgentVersionCRDSchema(t *testing.T) {
	data, err := os.ReadFile("../../config/crd/bases/monitoring.whatap.com_whatapagents.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var crd map[string]interface{}
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatal(err)
	}
	versions, _, err := unstructured.NestedSlice(crd, "spec", "versions")
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range versions {
		version := raw.(map[string]interface{})
		if version["name"] != "v2alpha1" {
			continue
		}
		field, found, err := unstructured.NestedMap(version, "schema", "openAPIV3Schema", "properties", "spec", "properties", "features", "properties", "kubeVirt", "properties", "targets", "items", "properties", "agentVersion")
		if err != nil || !found || field["type"] != "string" || field["maxLength"] != float64(32) {
			t.Fatal("CRD must preserve agentVersion and bound its length")
		}
		// A schema default is written into every CR and would pin omitted
		// targets to one release across operator upgrades.
		if _, pinned := field["default"]; pinned {
			t.Fatal("agentVersion must not have a schema default; the operator resolves an omitted value")
		}
		pattern, ok := field["pattern"].(string)
		if !ok {
			t.Fatal("release validation missing from CRD")
		}
		expr, err := regexp.Compile(pattern)
		if err != nil || !expr.MatchString("2.9.26") || expr.MatchString("2.9.25\n") || expr.MatchString("../2.9.25") || expr.MatchString("latest") {
			t.Fatal("CRD permits a non-release version")
		}
		return
	}
	t.Fatal("v2alpha1 schema missing")
}
