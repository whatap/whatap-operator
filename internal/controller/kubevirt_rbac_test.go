package controller

import (
	"os"
	"reflect"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

// KubeVirt bootstrap needs Secret create/get/patch for admission, and Secret
// list/delete plus VM/VMI list for the collector of unused Secrets. Secrets are
// never watched or updated, and KubeVirt objects are never written.
func TestKubeVirtBootstrapRBACStaysMinimal(t *testing.T) {
	data, err := os.ReadFile("../../config/rbac/role.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var role rbacv1.ClusterRole
	if err := yaml.Unmarshal(data, &role); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"secrets":                             {"create", "delete", "get", "list", "patch"},
		"kubevirt.io/virtualmachines":         {"list"},
		"kubevirt.io/virtualmachineinstances": {"list"},
	}
	found := map[string][]string{}
	for _, rule := range role.Rules {
		for _, group := range rule.APIGroups {
			for _, resource := range rule.Resources {
				key := resource
				if group != "" {
					key = group + "/" + resource
				}
				if _, tracked := want[key]; !tracked {
					continue
				}
				if _, duplicate := found[key]; duplicate {
					t.Fatalf("%s is granted by more than one rule", key)
				}
				found[key] = rule.Verbs
			}
		}
	}
	if !reflect.DeepEqual(found, want) {
		t.Fatalf("KubeVirt bootstrap privileges changed: got %v, want %v", found, want)
	}
}
