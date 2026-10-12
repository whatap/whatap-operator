package kubevirt

import (
	"testing"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func policyFixture() monitoringv2alpha1.KubeVirtSpec {
	return monitoringv2alpha1.KubeVirtSpec{Enabled: true, Targets: []monitoringv2alpha1.KubeVirtTargetSpec{{
		Name: "new-linux", Enabled: true, Namespace: "vms",
		VMSelector:       metav1.LabelSelector{MatchLabels: map[string]string{"app": "example"}},
		LicenseSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "server-license"}, Key: "license"},
		Host:             "collector.example/192.0.2.1", Port: 6600,
	}}}
}

func TestSelectTargetRequiresExplicitOptInAndExactNamespace(t *testing.T) {
	policy := policyFixture()
	for _, tc := range []struct {
		name      string
		namespace string
		labels    map[string]string
		want      bool
	}{
		{"selected", "vms", map[string]string{OptInLabel: "true", "app": "example"}, true},
		{"not opted in", "vms", map[string]string{"app": "example"}, false},
		{"wrong namespace", "other", map[string]string{OptInLabel: "true", "app": "example"}, false},
		{"wrong labels", "vms", map[string]string{OptInLabel: "true", "app": "other"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SelectTarget(policy, tc.namespace, tc.labels)
			if err != nil || (got != nil) != tc.want {
				t.Fatalf("selected=%v err=%v", got != nil, err)
			}
		})
	}
}

func TestValidatePolicyRejectsUnsafeInputsAndOverlaps(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*monitoringv2alpha1.KubeVirtTargetSpec)
	}{
		{"namespace", func(t *monitoringv2alpha1.KubeVirtTargetSpec) { t.Namespace = "" }},
		{"license ref", func(t *monitoringv2alpha1.KubeVirtTargetSpec) { t.LicenseSecretRef.Name = "" }},
		{"license key", func(t *monitoringv2alpha1.KubeVirtTargetSpec) { t.LicenseSecretRef.Key = "../license" }},
		{"host injection", func(t *monitoringv2alpha1.KubeVirtTargetSpec) { t.Host = "collector\nlicense=bad" }},
		{"shell host", func(t *monitoringv2alpha1.KubeVirtTargetSpec) { t.Host = "$(id)" }},
		{"port", func(t *monitoringv2alpha1.KubeVirtTargetSpec) { t.Port = 65536 }},
		{"selector", func(t *monitoringv2alpha1.KubeVirtTargetSpec) {
			t.VMSelector.MatchExpressions = []metav1.LabelSelectorRequirement{{Key: "x", Operator: "Unknown"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := policyFixture()
			tc.mutate(&policy.Targets[0])
			if ValidatePolicy(policy) == nil {
				t.Fatal("accepted invalid target")
			}
		})
	}
	policy := policyFixture()
	policy.Targets = append(policy.Targets, policy.Targets[0])
	policy.Targets[1].Name = "second"
	if _, err := SelectTarget(policy, "vms", map[string]string{OptInLabel: "true", "app": "example"}); err == nil {
		t.Fatal("ambiguous target silently won")
	}
	policy.Enabled = false
	policy.Targets[0].Host = ""
	if err := ValidatePolicy(policy); err != nil {
		t.Fatalf("disabled feature: %v", err)
	}
}
