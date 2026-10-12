package kubevirt

import (
	"strings"
	"testing"
)

func TestAgentVersionDefaultsAndOverridesForSelectedNewVM(t *testing.T) {
	for _, version := range []string{"", "2.9.25", "2.9.26", "10.12.123"} {
		t.Run("version="+version, func(t *testing.T) {
			policy := policyFixture()
			policy.Targets[0].AgentVersion = version
			got, err := SelectTarget(policy, "vms", map[string]string{OptInLabel: "true", "app": "example"})
			want := version
			if want == "" {
				want = "2.9.25"
			}
			if err != nil || got == nil || got.AgentVersion != want {
				t.Fatal("selected target did not resolve the requested/default tar release")
			}
			if policy.Targets[0].AgentVersion != version {
				t.Fatal("selecting a default mutated the original policy")
			}
		})
	}
}

func TestAgentVersionRejectsNonReleaseAndExecutableInput(t *testing.T) {
	for _, version := range []string{"latest", "v2.9.25", "2.9", "2.9.25\n", " 2.9.25", "../2.9.25", "2.9.25/extra", "2.9.25';id;#", "$(id)", strings.Repeat("1", 33) + ".1.1"} {
		t.Run("invalid="+version, func(t *testing.T) {
			policy := policyFixture()
			policy.Targets[0].AgentVersion = version
			err := ValidatePolicy(policy)
			if err == nil {
				t.Fatal("invalid release accepted")
			}
			if strings.Contains(err.Error(), version) {
				t.Fatal("untrusted version echoed in validation error")
			}
		})
	}
}
