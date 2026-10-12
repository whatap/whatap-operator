package controller

import (
	"os"
	"reflect"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

func TestKubeVirtBootstrapRBACOnlyNeedsSecretGetAndCreate(t *testing.T) {
	data, err := os.ReadFile("../../config/rbac/role.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var role rbacv1.ClusterRole
	if err := yaml.Unmarshal(data, &role); err != nil {
		t.Fatal(err)
	}
	for _, rule := range role.Rules {
		if reflect.DeepEqual(rule.APIGroups, []string{""}) && reflect.DeepEqual(rule.Resources, []string{"secrets"}) {
			if !reflect.DeepEqual(rule.Verbs, []string{"create", "get"}) {
				t.Fatal("bootstrap secret privileges were broadened")
			}
			return
		}
	}
	t.Fatal("missing explicit Secret get/create rule")
}
