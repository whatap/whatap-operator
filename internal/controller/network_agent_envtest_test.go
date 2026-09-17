package controller

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// This starts an isolated local API server, never the user's kubeconfig/cluster.
// make test supplies the pinned envtest binaries through KUBEBUILDER_ASSETS.
func TestNetworkAgentAPIServerLifecycle(t *testing.T) {
	assets := os.Getenv("KUBEBUILDER_ASSETS")
	if assets == "" {
		t.Skip("run make test to supply local envtest binaries")
	}
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := monitoringv2alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	env := &envtest.Environment{
		UseExistingCluster: boolPtr(false), BinaryAssetsDirectory: assets,
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true, Scheme: scheme,
		ControlPlaneStartTimeout: 40 * time.Second, ControlPlaneStopTimeout: 20 * time.Second,
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := env.Stop(); err != nil {
			t.Error(err)
		}
	})
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ns := "network-agent-envtest"
	if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
		t.Fatal(err)
	}
	cr := &monitoringv2alpha1.WhatapAgent{ObjectMeta: metav1.ObjectMeta{Name: "network-envtest"}}
	cr.Spec.Features.NetworkAgent = monitoringv2alpha1.NetworkAgentSpec{Enabled: true, Image: "example.invalid/network:v1", Stdout: "none", LogLevel: "info", LogInterval: "1m"}
	cr.Spec.Features.NetworkAgent.Tolerations = []corev1.Toleration{{Key: "canary", Value: "true", Effect: corev1.TaintEffectNoSchedule}}
	if err := json.Unmarshal([]byte(networkEnvFeature), &cr.Spec.Features.NetworkAgent); err != nil {
		t.Fatal(err)
	}
	var wantedFields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(networkEnvFeature), &wantedFields)
	if err := c.Create(ctx, cr); err != nil {
		t.Fatal(err)
	}
	stored := &monitoringv2alpha1.WhatapAgent{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(cr), stored); err != nil {
		t.Fatal(err)
	}
	logging := stored.Spec.Features.NetworkAgent
	persistedEnv, _ := json.Marshal(logging)
	var persistedFields map[string]json.RawMessage
	_ = json.Unmarshal(persistedEnv, &persistedFields)
	for _, field := range []string{"env", "envFrom"} {
		if !reflect.DeepEqual(persistedFields[field], wantedFields[field]) {
			t.Fatalf("API server pruned or changed networkAgent.%s", field)
		}
	}

	if logging.Stdout != "none" || logging.LogLevel != "info" || logging.LogInterval != "1m" {
		t.Fatal("API server pruned or changed networkAgent logging fields")
	}
	for _, field := range []string{"stdout", "logLevel"} {
		invalid := stored.DeepCopy()
		invalid.ObjectMeta = metav1.ObjectMeta{Name: "network-invalid-" + strings.ToLower(field)}
		if field == "stdout" {
			invalid.Spec.Features.NetworkAgent.Stdout = "off"
		} else {
			invalid.Spec.Features.NetworkAgent.LogLevel = "verbose"
		}
		if err := c.Create(ctx, invalid); !apierrors.IsInvalid(err) {
			t.Fatalf("API server accepted invalid logging %s: %v", field, err)
		}
	}
	r := &WhatapAgentReconciler{Client: c, Scheme: scheme, DefaultNamespace: ns}
	if err := r.reconcileNetworkAgent(ctx, cr); err != nil {
		t.Fatal(err)
	}
	first := networkDS(t, r)
	for i := 0; i < 3; i++ {
		if err := r.reconcileNetworkAgent(ctx, cr); err != nil {
			t.Fatal(err)
		}
	}
	after := networkDS(t, r)
	if first.ResourceVersion != after.ResourceVersion || !reflect.DeepEqual(first.Spec, after.Spec) {
		beforeJSON, _ := json.Marshal(first.Spec)
		afterJSON, _ := json.Marshal(after.Spec)
		t.Fatalf("API defaulting caused repeated DaemonSet writes: rv %s -> %s\nbefore=%s\nafter=%s", first.ResourceVersion, after.ResourceVersion, beforeJSON, afterJSON)
	}
	cr.Spec.Features.NetworkAgent.Image = "example.invalid/network:v2"
	if err := r.reconcileNetworkAgent(ctx, cr); err != nil {
		t.Fatal(err)
	}
	if networkDS(t, r).Spec.Template.Spec.Containers[0].Image != "example.invalid/network:v2" {
		t.Fatal("image update was not persisted")
	}
	cr.Spec.Features.NetworkAgent.Enabled = false
	if err := r.reconcileNetworkAgent(ctx, cr); err != nil {
		t.Fatal(err)
	}
	for _, obj := range r.networkAgentResources(cr) {
		if err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj); !apierrors.IsNotFound(err) {
			t.Fatalf("owned resource remains after disabling: %T %v", obj, err)
		}
	}
	cr.Spec.Features.NetworkAgent.Enabled = true
	if err := r.reconcileNetworkAgent(ctx, cr); err != nil {
		t.Fatal(err)
	}
	if networkDS(t, r).UID == first.UID {
		t.Fatal("reenable did not recreate the deleted DaemonSet")
	}
	// The envtest has no kubelet or DaemonSet controller, so this asserts API
	// lifecycle/defaulting, not real-kernel collection or live Pod readiness.
}
