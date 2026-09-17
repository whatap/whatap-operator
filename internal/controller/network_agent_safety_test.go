package controller

import (
	"context"
	"errors"
	"reflect"
	"testing"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNetworkAgentForeignRBACCollisions(t *testing.T) {
	for index := 1; index < 4; index++ {
		r, cr := networkFixture(t, `{"enabled":true,"image":"example.invalid/network:v1"}`)
		foreign := r.networkAgentResources(cr)[index]
		t.Run(reflect.TypeOf(foreign).String(), func(t *testing.T) {
			ctx := context.Background()
			foreign.SetLabels(map[string]string{"owner": "external"})
			if err := r.Create(ctx, foreign); err != nil {
				t.Fatal(err)
			}
			before := foreign.DeepCopyObject()
			if err := r.reconcileNetworkAgent(ctx, cr); err == nil {
				t.Fatal("foreign RBAC object was adopted")
			}
			if err := r.Get(ctx, client.ObjectKeyFromObject(foreign), foreign); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, foreign) {
				t.Fatal("foreign RBAC object was changed")
			}
			ds := &appsv1.DaemonSet{}
			if err := r.Get(ctx, client.ObjectKey{Name: networkAgentName, Namespace: r.DefaultNamespace}, ds); !apierrors.IsNotFound(err) {
				t.Fatal("collector created despite an ownership collision")
			}
		})
	}
}

type networkCleanupFailureClient struct {
	client.Client
	fail bool
	err  error
}

func (c *networkCleanupFailureClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if _, ok := obj.(*appsv1.DaemonSet); ok && obj.GetName() == networkAgentName && c.fail {
		return c.err
	}
	return c.Client.Delete(ctx, obj, opts...)
}

func TestNetworkAgentCleanupFailureRetainsFinalizer(t *testing.T) {
	ctx := context.Background()
	r, cr := networkFixture(t, `{"enabled":true,"image":"example.invalid/network:v1"}`)
	runNetworkReconcile(t, r, cr)
	stored := &monitoringv2alpha1.WhatapAgent{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(cr), stored); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, stored); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("injected network cleanup failure")
	failing := &networkCleanupFailureClient{Client: r.Client, fail: true, err: sentinel}
	r.Client = failing
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cr)}); !errors.Is(err, sentinel) {
		t.Fatalf("cleanup failure was hidden: %v", err)
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(cr), stored); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finalizer := range stored.Finalizers {
		found = found || finalizer == whatapFinalizer
	}
	if !found {
		t.Fatal("cleanup failure removed the CR finalizer")
	}
	failing.fail = false
	runNetworkReconcile(t, r, cr)
	if err := r.Get(ctx, client.ObjectKeyFromObject(cr), stored); !apierrors.IsNotFound(err) {
		t.Fatalf("successful retry did not finish CR deletion: %v", err)
	}
}

func TestNetworkAgentExternalAccountTransition(t *testing.T) {
	ctx := context.Background()
	external := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "external-network", Namespace: "whatap-monitoring"}}
	r, cr := networkFixture(t, `{"enabled":true,"image":"example.invalid/network:v1"}`, external)
	if err := r.reconcileNetworkAgent(ctx, cr); err != nil {
		t.Fatal(err)
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(external), external); err != nil {
		t.Fatal(err)
	}
	before := external.DeepCopy()
	cr.Spec.Features.NetworkAgent.ServiceAccountName = external.Name
	if err := r.reconcileNetworkAgent(ctx, cr); err != nil {
		t.Fatal(err)
	}
	if networkDS(t, r).Spec.Template.Spec.ServiceAccountName != external.Name {
		t.Fatal("account transition did not update the collector")
	}
	cr.Spec.Features.NetworkAgent.Enabled = false
	if err := r.reconcileNetworkAgent(ctx, cr); err != nil {
		t.Fatal(err)
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(external), external); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, external) {
		t.Fatal("external service account was mutated or adopted")
	}
	for _, obj := range r.networkAgentResources(cr) {
		if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); !apierrors.IsNotFound(err) {
			t.Fatalf("disable left old owned resource: %T %v", obj, err)
		}
	}
}

func TestNetworkAgentEnablePreservesOtherAgents(t *testing.T) {
	ctx := context.Background()
	r, cr := networkFixture(t, `{"enabled":false}`)
	cr.Spec.Features.K8sAgent.NodeAgent.Enabled = true
	cr.Spec.Features.K8sAgent.MasterAgent.Enabled = true
	if err := r.Update(ctx, cr); err != nil {
		t.Fatal(err)
	}
	runNetworkReconcile(t, r, cr)
	node := &appsv1.DaemonSet{}
	master := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKey{Name: "whatap-node-agent", Namespace: r.DefaultNamespace}, node); err != nil {
		t.Fatal(err)
	}
	if err := r.Get(ctx, client.ObjectKey{Name: "whatap-master-agent", Namespace: r.DefaultNamespace}, master); err != nil {
		t.Fatal(err)
	}
	beforeNode, beforeMaster := node.DeepCopy(), master.DeepCopy()
	if err := r.Get(ctx, client.ObjectKeyFromObject(cr), cr); err != nil {
		t.Fatal(err)
	}
	cr.Spec.Features.NetworkAgent.Enabled = true
	cr.Spec.Features.NetworkAgent.Image = "example.invalid/network:v1"
	cr.Spec.Features.NetworkAgent.ServiceAccountName = "whatap"
	if err := r.Update(ctx, cr); err != nil {
		t.Fatal(err)
	}
	runNetworkReconcile(t, r, cr)
	if err := r.Get(ctx, client.ObjectKeyFromObject(node), node); err != nil {
		t.Fatal(err)
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(master), master); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeNode.Spec, node.Spec) || !reflect.DeepEqual(beforeMaster.Spec, master.Spec) {
		t.Fatal("enabling the network collector changed existing agent pod templates")
	}
	_ = networkDS(t, r)
}
