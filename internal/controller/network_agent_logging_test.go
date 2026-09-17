package controller

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNetworkAgentLoggingArguments(t *testing.T) {
	base := []string{"-source=ebpf", "-output-mode=windows", "-export=tagcount", "-window=5s", "-pod-identity=true"}
	for _, tc := range []struct {
		name, fields string
		want         []string
	}{
		{"omitted preserves old image compatibility", "", nil},
		{"quiet info", `,"stdout":"none","logLevel":"info","logInterval":"1m"`, []string{"-stdout=none", "-log-level=info", "-log-interval=1m"}},
		{"debug does not imply telemetry stdout", `,"logLevel":"debug"`, []string{"-log-level=debug"}},
		{"explicit evidence", `,"stdout":"jsonl","logLevel":"warn","logInterval":"0s"`, []string{"-stdout=jsonl", "-log-level=warn", "-log-interval=0s"}},
		{"auto and error", `,"stdout":"auto","logLevel":"error"`, []string{"-stdout=auto", "-log-level=error"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, cr := networkFixture(t, `{"enabled":true,"image":"example.invalid/network:logging","serviceAccountName":"whatap"`+tc.fields+`}`)
			before := cr.DeepCopy()
			if err := r.reconcileNetworkAgent(context.Background(), cr); err != nil {
				t.Fatal(err)
			}
			want := append(append([]string(nil), base...), tc.want...)
			first := networkDS(t, r)
			if got := first.Spec.Template.Spec.Containers[0].Args; !reflect.DeepEqual(got, want) {
				t.Fatalf("args=%v want=%v", got, want)
			}
			if !reflect.DeepEqual(cr, before) {
				t.Fatal("logging defaulting mutated the CR")
			}
			if err := r.reconcileNetworkAgent(context.Background(), cr); err != nil {
				t.Fatal(err)
			}
			if second := networkDS(t, r); !reflect.DeepEqual(first.Spec.Template, second.Spec.Template) {
				t.Fatal("logging options cause template drift on repeated reconcile")
			}
		})
	}
}

func TestNetworkAgentLoggingRejectsInvalidBeforeWrites(t *testing.T) {
	for _, tc := range []struct{ field, value string }{
		{"stdout", "off"}, {"stdout", " jsonl"},
		{"logLevel", "verbose"}, {"logLevel", "INFO"},
		{"logInterval", "-1s"}, {"logInterval", "not-a-duration"},
		{"logInterval", "999999999999999999999h"},
	} {
		t.Run(tc.field+"="+tc.value, func(t *testing.T) {
			feature, err := json.Marshal(map[string]any{"enabled": true, "image": "example.invalid/network:logging", tc.field: tc.value})
			if err != nil {
				t.Fatal(err)
			}
			r, cr := networkFixture(t, string(feature))
			if err := r.reconcileNetworkAgent(context.Background(), cr); err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("invalid %s must be rejected with field-specific error: %v", tc.field, err)
			}
			for _, list := range []client.ObjectList{&appsv1.DaemonSetList{}, &corev1.ServiceAccountList{}, &rbacv1.ClusterRoleList{}, &rbacv1.ClusterRoleBindingList{}} {
				if err := r.List(context.Background(), list); err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(list)
				if err != nil {
					t.Fatal(err)
				}
				var decoded struct {
					Items []json.RawMessage `json:"items"`
				}
				if err := json.Unmarshal(b, &decoded); err != nil || len(decoded.Items) != 0 {
					t.Fatalf("invalid logging configuration wrote %T: %s (%v)", list, b, err)
				}
			}
		})
	}
}
