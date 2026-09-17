package controller

import (
	"context"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNetworkAgentMemoryLimitTracksResources(t *testing.T) {
	for _, tc := range []struct{ limit, want string }{{"256Mi", "192MiB"}, {"1Gi", "768MiB"}} {
		t.Run(tc.limit, func(t *testing.T) {
			r, cr := networkFixture(t, `{"enabled":true,"image":"example.invalid/network:v1","serviceAccountName":"whatap"}`)
			cr.Spec.Features.NetworkAgent.Resources.Limits = corev1.ResourceList{corev1.ResourceMemory: resourceMustParse(tc.limit)}
			before := cr.DeepCopy()
			if err := r.reconcileNetworkAgent(context.Background(), cr); err != nil {
				t.Fatal(err)
			}
			var got string
			for _, e := range networkDS(t, r).Spec.Template.Spec.Containers[0].Env {
				if e.Name == "GOMEMLIMIT" {
					got = e.Value
				}
			}
			if got != tc.want {
				t.Fatalf("GOMEMLIMIT=%q want %q for memory limit %s", got, tc.want, tc.limit)
			}
			if !reflect.DeepEqual(cr, before) {
				t.Fatal("resource defaulting mutated the CR")
			}
		})
	}
}

func TestNetworkAgentSchedulesLinuxOnly(t *testing.T) {
	r, cr := networkFixture(t, `{"enabled":true,"image":"example.invalid/network:v1","serviceAccountName":"whatap","nodeSelector":{"canary":"true"}}`)
	before := cr.DeepCopy()
	if err := r.reconcileNetworkAgent(context.Background(), cr); err != nil {
		t.Fatal(err)
	}
	nodes := networkDS(t, r).Spec.Template.Spec.NodeSelector
	if nodes["kubernetes.io/os"] != "linux" || nodes["canary"] != "true" {
		t.Fatalf("Linux-only selector missing or custom selector lost: %v", nodes)
	}
	if !reflect.DeepEqual(cr, before) {
		t.Fatal("selector defaulting mutated the CR")
	}
}

func TestNetworkAgentRejectsInvalidResourcesAndOS(t *testing.T) {
	for _, tc := range []struct{ name, feature string }{
		{"windows", `{"enabled":true,"image":"example.invalid/network:v1","nodeSelector":{"kubernetes.io/os":"windows"}}`},
		{"zero-memory", `{"enabled":true,"image":"example.invalid/network:v1","resources":{"limits":{"memory":"0"}}}`},
		{"request-over-limit", `{"enabled":true,"image":"example.invalid/network:v1","resources":{"requests":{"memory":"1Gi"},"limits":{"memory":"512Mi"}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, cr := networkFixture(t, tc.feature)
			if err := r.reconcileNetworkAgent(context.Background(), cr); err == nil {
				t.Fatal("invalid network configuration accepted")
			}
			ds := &appsv1.DaemonSet{}
			if err := r.Get(context.Background(), client.ObjectKey{Namespace: r.DefaultNamespace, Name: networkAgentName}, ds); !apierrors.IsNotFound(err) {
				t.Fatal("invalid configuration created a DaemonSet")
			}
			sa := &corev1.ServiceAccount{}
			if err := r.Get(context.Background(), client.ObjectKey{Namespace: r.DefaultNamespace, Name: networkAgentName}, sa); !apierrors.IsNotFound(err) {
				t.Fatal("invalid configuration created a ServiceAccount")
			}
		})
	}
}
