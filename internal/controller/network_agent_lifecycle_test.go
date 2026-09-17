package controller

import (
	"context"
	"reflect"
	"testing"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNetworkAgentOwnedLifecycle(t *testing.T) {
	ctx := context.Background()
	r, cr := networkFixture(t, `{"enabled":true,"image":"example.invalid/network:v1"}`)
	runNetworkReconcile(t, r, cr)
	sa := &corev1.ServiceAccount{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: r.DefaultNamespace, Name: networkAgentName}, sa); err != nil {
		t.Fatal(err)
	}
	if !metav1.IsControlledBy(sa, cr) {
		t.Fatal("SA owner")
	}
	roles := &rbacv1.ClusterRoleList{}
	if err := r.List(ctx, roles); err != nil {
		t.Fatal(err)
	}
	rules := []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list", "watch"}}}
	if len(roles.Items) != 1 || !reflect.DeepEqual(roles.Items[0].Rules, rules) || !metav1.IsControlledBy(&roles.Items[0], cr) {
		t.Fatal("minimal owned role missing")
	}
	bindings := &rbacv1.ClusterRoleBindingList{}
	if err := r.List(ctx, bindings); err != nil {
		t.Fatal(err)
	}
	if len(bindings.Items) != 1 || !metav1.IsControlledBy(&bindings.Items[0], cr) || bindings.Items[0].RoleRef.Name != roles.Items[0].Name || !reflect.DeepEqual(bindings.Items[0].Subjects, []rbacv1.Subject{{Kind: "ServiceAccount", Name: networkAgentName, Namespace: r.DefaultNamespace}}) {
		t.Fatal("binding")
	}
	before := networkDS(t, r)
	if err := r.reconcileNetworkAgent(ctx, cr); err != nil {
		t.Fatal(err)
	}
	if networkDS(t, r).ResourceVersion != before.ResourceVersion {
		t.Fatal("non-idempotent")
	}
	before.Spec.Template.Spec.Containers[0].Args = []string{"-duration=1s"}
	if err := r.Update(ctx, before); err != nil {
		t.Fatal(err)
	}
	cr.Spec.Features.NetworkAgent.Image = "example.invalid/network:v2"
	cr.Spec.Features.NetworkAgent.CredentialsSecretName = "custom-credentials"
	cr.Spec.Features.NetworkAgent.Resources.Requests = corev1.ResourceList{corev1.ResourceCPU: resourceMustParse("75m")}
	cr.Spec.Features.NetworkAgent.NodeSelector = map[string]string{"canary": "true"}
	cr.Spec.Features.NetworkAgent.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "registry"}}
	cr.Spec.Features.NetworkAgent.Tolerations = []corev1.Toleration{{Operator: corev1.TolerationOpExists}}
	if err := r.reconcileNetworkAgent(ctx, cr.DeepCopy()); err != nil {
		t.Fatal(err)
	}
	after := networkDS(t, r)
	p := after.Spec.Template.Spec
	c := p.Containers[0]
	if c.Image != "example.invalid/network:v2" || len(c.Args) != 5 || c.Env[2].ValueFrom.SecretKeyRef.Name != "custom-credentials" || c.Resources.Requests.Cpu().String() != "75m" || p.NodeSelector["canary"] != "true" || len(p.ImagePullSecrets) != 1 || len(p.Tolerations) != 1 {
		t.Fatal("desired drift not reconciled")
	}
	if err := r.Delete(ctx, after); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileNetworkAgent(ctx, cr); err != nil {
		t.Fatal(err)
	}
	_ = networkDS(t, r)
	other := cr.DeepCopy()
	other.Name = "other"
	other.UID = types.UID("other-uid")
	other.Spec.Features.NetworkAgent.Enabled = false
	if err := r.reconcileNetworkAgent(ctx, other); err != nil {
		t.Fatal(err)
	}
	_ = networkDS(t, r)
	cr.Spec.Features.NetworkAgent.Enabled = false
	if err := r.reconcileNetworkAgent(ctx, cr); err != nil {
		t.Fatal(err)
	}
	for _, obj := range []client.Object{&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: networkAgentName, Namespace: r.DefaultNamespace}}, sa, &roles.Items[0], &bindings.Items[0]} {
		if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); !apierrors.IsNotFound(err) {
			t.Fatalf("owned resource not cleaned: %T %v", obj, err)
		}
	}
	cr.Spec.Features.NetworkAgent.Enabled = true
	if err := r.reconcileNetworkAgent(ctx, cr); err != nil {
		t.Fatal(err)
	}
	_ = networkDS(t, r)
	cr.Spec.Features.NetworkAgent.Enabled = false
	if err := r.reconcileNetworkAgent(ctx, cr); err != nil {
		t.Fatal(err)
	}
	cr.Spec.Features.NetworkAgent.Enabled = true
	foreign := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: networkAgentName, Namespace: r.DefaultNamespace}}
	if err := r.Create(ctx, foreign); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileNetworkAgent(ctx, cr); err == nil {
		t.Fatal("foreign DS adopted")
	}
	if len(networkDS(t, r).OwnerReferences) != 0 {
		t.Fatal("foreign ownership mutated")
	}
}

func TestNetworkAgentRequiresImage(t *testing.T) {
	r, cr := networkFixture(t, `{"enabled":true}`)
	if err := r.reconcileNetworkAgent(context.Background(), cr); err == nil {
		t.Fatal("empty image accepted")
	}
}

func TestNetworkAgentDisabledPreservesForeign(t *testing.T) {
	for _, feature := range []string{`{}`, `{"enabled":false}`} {
		foreign := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: networkAgentName, Namespace: "whatap-monitoring"}}
		node := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "whatap-node-agent", Namespace: "whatap-monitoring"}}
		r, cr := networkFixture(t, feature, foreign, node)
		if err := r.reconcileNetworkAgent(context.Background(), cr); err != nil {
			t.Fatal(err)
		}
		ds := networkDS(t, r)
		if ds.ResourceVersion != foreign.ResourceVersion && foreign.ResourceVersion != "" {
			t.Fatal("foreign changed")
		}
		for _, obj := range []client.Object{foreign, node} {
			if err := r.Get(context.Background(), client.ObjectKeyFromObject(obj), obj); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestNetworkAgentCRDeletion(t *testing.T) {
	r, cr := networkFixture(t, `{"enabled":true,"image":"example.invalid/network:v1"}`)
	runNetworkReconcile(t, r, cr)
	stored := &monitoringv2alpha1.WhatapAgent{}
	if err := r.Get(context.Background(), client.ObjectKeyFromObject(cr), stored); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(context.Background(), stored); err != nil {
		t.Fatal(err)
	}
	runNetworkReconcile(t, r, cr)
	ds := &appsv1.DaemonSet{}
	if err := r.Get(context.Background(), client.ObjectKey{Name: networkAgentName, Namespace: r.DefaultNamespace}, ds); !apierrors.IsNotFound(err) {
		t.Fatalf("CR delete left network DS: %v", err)
	}
}
