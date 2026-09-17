package controller

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"
)

func TestNetworkAgentMaxSurgeCompat(t *testing.T) {
	const feature = `{"enabled":true,"image":"example.invalid/network:v1","serviceAccountName":"whatap"}`
	ctx := context.Background()
	t.Run("first create omits unsupported field", func(t *testing.T) {
		r, cr := networkFixture(t, feature)
		if err := r.reconcileNetworkAgent(ctx, cr); err != nil {
			t.Fatal(err)
		}
		if got := networkDS(t, r).Spec.UpdateStrategy.RollingUpdate.MaxSurge; got != nil {
			t.Fatalf("first create MaxSurge = %v, want nil for Kubernetes 1.19", *got)
		}
	})
	for _, tc := range []struct {
		name  string
		surge *intstr.IntOrString
	}{
		{name: "old server pruned nil"},
		{name: "new server defaulted zero", surge: func() *intstr.IntOrString { v := intstr.FromInt32(0); return &v }()},
		{name: "supported nonzero", surge: func() *intstr.IntOrString { v := intstr.FromInt32(2); return &v }()},
		{name: "supported percentage", surge: func() *intstr.IntOrString { v := intstr.FromString("25%"); return &v }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, cr := networkFixture(t, feature)
			if err := r.reconcileNetworkAgent(ctx, cr); err != nil {
				t.Fatal(err)
			}
			ds := networkDS(t, r)
			ds.Spec.UpdateStrategy.RollingUpdate.MaxSurge = tc.surge
			if err := r.Update(ctx, ds); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				before := networkDS(t, r).ResourceVersion
				if err := r.reconcileNetworkAgent(ctx, cr); err != nil {
					t.Fatal(err)
				}
				ds = networkDS(t, r)
				got := ds.Spec.UpdateStrategy.RollingUpdate.MaxSurge
				if tc.surge == nil {
					if got != nil {
						t.Fatalf("reconcile %d: old-server MaxSurge = %v, want nil", i, *got)
					}
				} else if got == nil || *got != intstr.FromInt32(0) {
					t.Fatalf("reconcile %d: supported MaxSurge = %v, want explicit zero", i, got)
				}
				if (i > 0 || tc.surge == nil || *tc.surge == intstr.FromInt32(0)) && ds.ResourceVersion != before {
					t.Fatalf("reconcile %d unnecessarily updated DaemonSet: %s -> %s", i, before, ds.ResourceVersion)
				}
			}
		})
	}
}

func TestNetworkAgentCRDCompat(t *testing.T) {
	data, err := os.ReadFile("../../config/crd/bases/monitoring.whatap.com_whatapagents.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, version := range crd.Spec.Versions {
		if version.Name != "v2alpha1" {
			continue
		}
		if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
			t.Fatal("missing v2alpha1 schema")
		}
		network, ok := version.Schema.OpenAPIV3Schema.Properties["spec"].Properties["features"].Properties["networkAgent"]
		if !ok {
			t.Fatal("missing networkAgent schema")
		}
		found = true
		encoded, err := json.Marshal(network)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), `"x-kubernetes-validations":`) {
			t.Fatal("networkAgent schema contains CEL validation unsupported by Kubernetes 1.19")
		}
	}
	if !found {
		t.Fatal("missing v2alpha1 networkAgent schema")
	}
}

func TestNetworkAgentImageRuntimeCompat(t *testing.T) {
	for _, feature := range []string{`{"enabled":true}`, `{"enabled":true,"image":"   "}`} {
		r, cr := networkFixture(t, feature)
		err := r.reconcileNetworkAgent(context.Background(), cr)
		if err == nil || !strings.Contains(err.Error(), "networkAgent.image is required when enabled") {
			t.Fatalf("enabled agent without image must still be rejected by controller, got %v", err)
		}
	}
}
