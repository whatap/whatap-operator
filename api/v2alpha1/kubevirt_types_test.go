package v2alpha1

import (
	"encoding/json"
	"testing"
)

func TestKubeVirtPolicySurvivesRoundTrip(t *testing.T) {
	input := []byte(`{"spec":{"features":{"kubeVirt":{"enabled":true,"targets":[{"name":"new-linux","enabled":true,"namespace":"vms","vmSelector":{"matchLabels":{"app":"example"}},"licenseSecretRef":{"name":"server-license","key":"license"},"host":"collector.example","port":6600,"agentVersion":"2.9.26"}]}}}}`)
	var cr WhatapAgent
	if err := json.Unmarshal(input, &cr); err != nil {
		t.Fatal(err)
	}
	copy := cr.DeepCopy()
	encoded, err := json.Marshal(copy)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	features := got["spec"].(map[string]interface{})["features"].(map[string]interface{})
	policy, ok := features["kubeVirt"].(map[string]interface{})
	if !ok || policy["enabled"] != true {
		t.Fatal("kubeVirt policy was lost during typed decode/deep-copy/encode")
	}
	target := policy["targets"].([]interface{})[0].(map[string]interface{})
	if target["namespace"] != "vms" || target["host"] != "collector.example" {
		t.Fatal("KubeVirt target was changed during round trip")
	}
	if target["agentVersion"] != "2.9.26" {
		t.Fatal("agentVersion was lost during typed decode/deep-copy/encode")
	}
}
