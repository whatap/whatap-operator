package controller

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func networkFixture(t *testing.T, feature string, objects ...client.Object) (*WhatapAgentReconciler, *monitoringv2alpha1.WhatapAgent) {
	t.Helper()
	cr := &monitoringv2alpha1.WhatapAgent{ObjectMeta: metav1.ObjectMeta{Name: "network-test", UID: types.UID("network-test-uid")}}
	if err := json.Unmarshal([]byte(`{"networkAgent":`+feature+`}`), &cr.Spec.Features); err != nil {
		t.Fatal(err)
	}
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := monitoringv2alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	objects = append(objects, cr)
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(cr).WithObjects(objects...).Build()
	return &WhatapAgentReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(100), DefaultNamespace: "whatap-monitoring"}, cr
}

func runNetworkReconcile(t *testing.T, r *WhatapAgentReconciler, cr *monitoringv2alpha1.WhatapAgent) {
	t.Helper()
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cr)}); err != nil {
		t.Fatal(err)
	}
}

func networkDS(t *testing.T, r *WhatapAgentReconciler) *appsv1.DaemonSet {
	t.Helper()
	ds := &appsv1.DaemonSet{}
	if err := r.Get(context.Background(), client.ObjectKey{Namespace: r.DefaultNamespace, Name: "whatap-network-agent"}, ds); err != nil {
		t.Fatal(err)
	}
	return ds
}

func TestNetworkAgentStandaloneContract(t *testing.T) {
	r, cr := networkFixture(t, `{"enabled":true,"image":"example.invalid/network:v1","serviceAccountName":"whatap"}`)
	runNetworkReconcile(t, r, cr)
	ds := networkDS(t, r)
	if !metav1.IsControlledBy(ds, cr) {
		t.Fatal("missing CR UID ownership")
	}
	p := ds.Spec.Template.Spec
	if len(p.Containers) != 1 || len(p.InitContainers) != 0 {
		t.Fatal("network agent must be standalone")
	}
	c := p.Containers[0]
	if c.Image != "example.invalid/network:v1" {
		t.Fatal(c.Image)
	}
	wantArgs := []string{"-source=ebpf", "-output-mode=windows", "-export=tagcount", "-window=5s", "-pod-identity=true"}
	if !reflect.DeepEqual(c.Args, wantArgs) {
		t.Fatalf("args = %v", c.Args)
	}
	if !p.HostPID || !p.HostNetwork || p.DNSPolicy != corev1.DNSClusterFirstWithHostNet || p.AutomountServiceAccountToken == nil || !*p.AutomountServiceAccountToken || p.ServiceAccountName != "whatap" {
		t.Fatalf("pod contract: %+v", p)
	}
	s := c.SecurityContext
	if s == nil || s.RunAsUser == nil || *s.RunAsUser != 0 || s.Privileged == nil || !*s.Privileged || s.ReadOnlyRootFilesystem == nil || !*s.ReadOnlyRootFilesystem {
		t.Fatal("security contract")
	}
	if len(c.Env) != 7 || c.Env[0].Name != "NODE_NAME" || c.Env[0].ValueFrom.FieldRef.FieldPath != "spec.nodeName" || c.Env[1].Name != "WHATAP_OBJECT_NAME" || c.Env[1].Value != "network-$(NODE_NAME)" {
		t.Fatalf("env order = %+v", c.Env)
	}
	refs := map[string]string{"WHATAP_ACCESSKEY": "WHATAP_LICENSE", "WHATAP_SERVER_HOST": "WHATAP_HOST", "WHATAP_SERVER_PORT": "WHATAP_PORT"}
	for _, e := range c.Env {
		if key, ok := refs[e.Name]; ok {
			if e.Value != "" || e.ValueFrom == nil || e.ValueFrom.SecretKeyRef == nil || e.ValueFrom.SecretKeyRef.Name != "whatap-credentials" || e.ValueFrom.SecretKeyRef.Key != key {
				t.Fatalf("credential env %+v", e)
			}
			delete(refs, e.Name)
		}
	}
	if len(refs) != 0 {
		t.Fatal("missing credential refs")
	}
	if c.Env[5].Name != "GOMAXPROCS" || c.Env[5].Value != "1" || c.Env[6].Name != "GOMEMLIMIT" || c.Env[6].Value != "384MiB" {
		t.Fatal("runtime limits")
	}
	if c.Resources.Requests.Cpu().String() != "50m" || c.Resources.Requests.Memory().String() != "128Mi" || c.Resources.Limits.Cpu().String() != "500m" || c.Resources.Limits.Memory().String() != "512Mi" {
		t.Fatal(c.Resources)
	}
	paths := []string{"/sys/kernel/tracing", "/sys/kernel/debug", "/sys/kernel/btf"}
	if len(p.Volumes) != 3 || len(c.VolumeMounts) != 3 {
		t.Fatal("unexpected host access")
	}
	for i, path := range paths {
		if p.Volumes[i].HostPath == nil || p.Volumes[i].HostPath.Path != path || c.VolumeMounts[i].MountPath != path || !c.VolumeMounts[i].ReadOnly {
			t.Fatal("host mount contract")
		}
	}
	for _, list := range []client.ObjectList{&corev1.ServiceAccountList{}, &rbacv1.ClusterRoleList{}, &rbacv1.ClusterRoleBindingList{}} {
		if err := r.List(context.Background(), list); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(list)
		var decoded struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(b, &decoded)
		if len(decoded.Items) != 0 {
			t.Fatalf("explicit SA must not create RBAC: %s", b)
		}
	}
}
