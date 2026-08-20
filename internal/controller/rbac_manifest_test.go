package controller

import (
	"bytes"
	"io"
	"os"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

func TestAgentManifestAllowsAllNonResourceURLs(t *testing.T) {
	data, err := os.ReadFile("../../manifests/resources.yaml")
	if err != nil {
		t.Fatalf("read agent manifest: %v", err)
	}

	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		var object unstructured.Unstructured
		err := decoder.Decode(&object)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode agent manifest: %v", err)
		}
		if object.GetKind() != "ClusterRole" || object.GetName() != "whatap" {
			continue
		}

		rules, found, err := unstructured.NestedSlice(object.Object, "rules")
		if err != nil || !found {
			t.Fatalf("read whatap ClusterRole rules: found=%v err=%v", found, err)
		}
		for _, rawRule := range rules {
			rule, ok := rawRule.(map[string]interface{})
			if !ok {
				continue
			}
			urls, ok := rule["nonResourceURLs"].([]interface{})
			if !ok {
				continue
			}
			if len(urls) != 1 || urls[0] != "*" {
				t.Fatalf("expected wildcard non-resource URL, got %v", urls)
			}
			return
		}
		t.Fatal("expected a non-resource URL rule in whatap ClusterRole")
	}

	t.Fatal("whatap ClusterRole not found")
}
