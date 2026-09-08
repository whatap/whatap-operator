package controller

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

func TestGpuPodResourcesPathCRD(t *testing.T) {
	for _, path := range []string{
		"../../config/crd/bases/monitoring.whatap.com_whatapagents.yaml",
		"../../manifests/crd.yaml",
	} {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
			foundSchema := false
			for {
				var object unstructured.Unstructured
				if err := decoder.Decode(&object); err == io.EOF {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				if object.GetKind() != "CustomResourceDefinition" || object.GetName() != "whatapagents.monitoring.whatap.com" {
					continue
				}
				versions, _, err := unstructured.NestedSlice(object.Object, "spec", "versions")
				if err != nil {
					t.Fatal(err)
				}
				for _, raw := range versions {
					version := raw.(map[string]interface{})
					if version["name"] != "v2alpha1" {
						continue
					}
					gpuSchema, ok, err := unstructured.NestedMap(version, "schema", "openAPIV3Schema", "properties", "spec", "properties", "features", "properties", "k8sAgent", "properties", "gpuMonitoring")
					if err != nil || !ok {
						t.Fatalf("gpuMonitoring schema missing: %v", err)
					}
					field, ok, err := unstructured.NestedMap(gpuSchema, "properties", "podResourcesPath")
					if err != nil || !ok {
						t.Fatalf("podResourcesPath schema missing: %v", err)
					}
					if field["type"] != "string" {
						t.Fatalf("podResourcesPath type = %v, want string", field["type"])
					}
					pattern, ok := field["pattern"].(string)
					if !ok {
						t.Fatal("absolute-path validation missing")
					}
					re, err := regexp.Compile(pattern)
					if err != nil {
						t.Fatal(err)
					}
					if !re.MatchString("/repo.p/kubelet/pod-resources") || !re.MatchString("") || re.MatchString("relative/pod-resources") {
						t.Fatalf("unexpected path validation: %q", pattern)
					}
					required, _, err := unstructured.NestedStringSlice(gpuSchema, "required")
					if err != nil {
						t.Fatal(err)
					}
					for _, name := range required {
						if name == "podResourcesPath" {
							t.Fatal("path must remain optional for existing CRs")
						}
					}
					foundSchema = true
				}
			}
			if !foundSchema {
				t.Fatal("WhatapAgent v2alpha1 schema not found")
			}
		})
	}
}
