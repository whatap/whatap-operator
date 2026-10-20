package v2alpha1

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/whatap/whatap-operator/internal/apmwebhook"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// Real API server test of the reachability probe and of the Events on Pods
// admitted without injection. The webhook server runs in this process. A
// caBundle from an unrelated CA makes the API server's webhook call fail x509
// verification, which is what a proxy intercepting its TLS connection causes.
// envtest has no Service networking, so the webhooks use URL client configs;
// the production Service-based configuration is covered by the controller test.
func TestAPMWebhookDiagnosticsWithLocalAPIServer(t *testing.T) {
	assets := os.Getenv("KUBEBUILDER_ASSETS")
	if assets == "" {
		t.Skip("set KUBEBUILDER_ASSETS to isolated envtest binaries")
	}
	useExisting := false
	scheme := newProbeTestScheme(t)
	if err := admissionregistrationv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	environment := &envtest.Environment{
		UseExistingCluster: &useExisting, BinaryAssetsDirectory: assets,
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true, Scheme: scheme,
		ControlPlaneStartTimeout: 40 * time.Second, ControlPlaneStopTimeout: 20 * time.Second,
	}
	cfg, err := environment.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Error(err)
		}
	})
	serving := environment.WebhookInstallOptions
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
		WebhookServer: webhook.NewServer(webhook.Options{
			Host: serving.LocalServingHost, Port: serving.LocalServingPort, CertDir: serving.LocalServingCertDir,
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	startedAt := time.Now()
	if err := SetupWhatapAgentWebhookWithManager(mgr); err != nil {
		t.Fatal(err)
	}
	if err := (&MissedInjectionEventReconciler{Client: mgr.GetClient(), NotBefore: startedAt}).SetupWithManager(mgr); err != nil {
		t.Fatal(err)
	}
	// Driven by hand below instead of on its production schedule.
	probe := newAPMWebhookProbe(mgr.GetClient(), probeTestNamespace)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	managerDone := make(chan error, 1)
	go func() { managerDone <- mgr.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-managerDone; err != nil {
			t.Error(err)
		}
	})
	eventually(t, ctx, "webhook server to serve", func() (bool, string) {
		err := mgr.GetWebhookServer().StartedChecker()(nil)
		return err == nil, fmt.Sprint(err)
	})

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	for _, namespace := range []string{probeTestNamespace, "app"} {
		if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}); err != nil {
			t.Fatal(err)
		}
		if err := c.Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: namespace}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Create(ctx, apmTestAgent(true)); err != nil {
		t.Fatal(err)
	}
	operatorCA := serving.LocalServingCAData
	webhookConfiguration := localPodWebhookConfiguration(serving.LocalServingHost, serving.LocalServingPort, operatorCA)
	if err := c.Create(ctx, webhookConfiguration); err != nil {
		t.Fatal(err)
	}

	// 1. Reachable: probes pass and target Pods are injected.
	waitForProbe(t, ctx, probe, apmWebhookReached)
	probe.Run(ctx)
	injected := createAppPod(t, ctx, c, "injected", "demo")
	if injected.Annotations[apmInjectedAnnotation] != "true" || len(injected.Spec.InitContainers) == 0 {
		t.Fatalf("target Pod was not injected while the webhook was reachable: %v", injected.Annotations)
	}

	// 2. The API server's webhook call fails x509 verification.
	setCABundle(t, ctx, c, unrelatedCABundle(t))
	cause := waitForProbe(t, ctx, probe, apmWebhookUnreachable)
	t.Logf("API server error returned to the probe: %s", cause)
	if !strings.Contains(cause, "x509") || !strings.Contains(cause, fmt.Sprintf("failed calling webhook %q", apmwebhook.ProbeWebhookName)) {
		t.Fatalf("probe cause = %q, want the API server's x509 call error", cause)
	}
	for range apmWebhookProbeFailureThreshold {
		probe.Run(ctx)
	}
	unreachable := eventsWithReason(t, ctx, c, metav1.NamespaceDefault, apmWebhookUnreachableReason)
	if len(unreachable) != 1 || unreachable[0].InvolvedObject.Kind != "WhatapAgent" || !strings.Contains(unreachable[0].Message, "x509") {
		t.Fatalf("unreachable events = %#v, want one x509 Warning on the WhatapAgent", unreachable)
	}
	t.Logf("WhatapAgent event: %s %s: %s", unreachable[0].Type, unreachable[0].Reason, unreachable[0].Message)

	missed := createAppPod(t, ctx, c, "missed", "demo")
	if _, marked := missed.Annotations[apmInjectedAnnotation]; marked || len(missed.Spec.InitContainers) != 0 {
		t.Fatal("mpod.kb.io no longer fails open")
	}
	createAppPod(t, ctx, c, "unrelated", "other")
	var podEvent corev1.Event
	eventually(t, ctx, "Warning Event on the Pod admitted without injection", func() (bool, string) {
		events := eventsWithReason(t, ctx, c, "app", apmInjectionMissingReason)
		if len(events) == 1 {
			podEvent = events[0]
		}
		return len(events) == 1, fmt.Sprintf("%d events", len(events))
	})
	if podEvent.InvolvedObject.Name != missed.Name || podEvent.InvolvedObject.UID != missed.UID {
		t.Fatalf("event is on %s, want Pod %s", podEvent.InvolvedObject.Name, missed.Name)
	}
	t.Logf("Pod event: %s %s: %s", podEvent.Type, podEvent.Reason, podEvent.Message)

	// 3. Recovery.
	setCABundle(t, ctx, c, operatorCA)
	waitForProbe(t, ctx, probe, apmWebhookReached)
	probe.Run(ctx)
	if recovered := eventsWithReason(t, ctx, c, metav1.NamespaceDefault, apmWebhookRecoveredReason); len(recovered) != 1 || recovered[0].Type != corev1.EventTypeNormal {
		t.Fatalf("recovered events = %#v, want one Normal event", recovered)
	}
	// The unrelated and injected Pods must never have been reported.
	if events := eventsWithReason(t, ctx, c, "app", apmInjectionMissingReason); len(events) != 1 {
		t.Fatalf("Pod events = %d, want only the missed Pod's", len(events))
	}
}

// localPodWebhookConfiguration mirrors the production mpod.kb.io and probe
// entries with URL client configs.
func localPodWebhookConfiguration(host string, port int, caBundle []byte) *admissionregistrationv1.MutatingWebhookConfiguration {
	url := "https://" + net.JoinHostPort(host, strconv.Itoa(port)) + apmwebhook.PodInjectionPath
	ignore, fail := admissionregistrationv1.Ignore, admissionregistrationv1.Fail
	sideEffects := admissionregistrationv1.SideEffectClassNone
	rules := []admissionregistrationv1.RuleWithOperations{{
		Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
		Rule:       admissionregistrationv1.Rule{APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"pods"}},
	}}
	return &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-webhook"},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			{
				Name: "mpod.kb.io", ClientConfig: admissionregistrationv1.WebhookClientConfig{URL: &url, CABundle: caBundle},
				Rules: rules, FailurePolicy: &ignore, SideEffects: &sideEffects, AdmissionReviewVersions: []string{"v1"},
			},
			{
				Name: apmwebhook.ProbeWebhookName, ClientConfig: admissionregistrationv1.WebhookClientConfig{URL: &url, CABundle: caBundle},
				Rules: rules, FailurePolicy: &fail, SideEffects: &sideEffects, AdmissionReviewVersions: []string{"v1"},
				ObjectSelector: &metav1.LabelSelector{MatchLabels: map[string]string{apmwebhook.ProbeLabel: "true"}},
			},
		},
	}
}

func setCABundle(t *testing.T, ctx context.Context, c client.Client, caBundle []byte) {
	t.Helper()
	configuration := &admissionregistrationv1.MutatingWebhookConfiguration{}
	if err := c.Get(ctx, client.ObjectKey{Name: "whatap-webhook"}, configuration); err != nil {
		t.Fatal(err)
	}
	for i := range configuration.Webhooks {
		configuration.Webhooks[i].ClientConfig.CABundle = caBundle
	}
	if err := c.Update(ctx, configuration); err != nil {
		t.Fatal(err)
	}
}

// waitForProbe waits until the API server has picked up the webhook
// configuration and the probe reports want.
func waitForProbe(t *testing.T, ctx context.Context, probe *apmWebhookProbe, want apmWebhookProbeOutcome) string {
	t.Helper()
	var detail string
	eventually(t, ctx, fmt.Sprintf("probe outcome %d", want), func() (bool, string) {
		var outcome apmWebhookProbeOutcome
		outcome, detail = probe.probe(ctx)
		return outcome == want, fmt.Sprintf("outcome %d: %s", outcome, detail)
	})
	return detail
}

func createAppPod(t *testing.T, ctx context.Context, c client.Client, name, app string) *corev1.Pod {
	t.Helper()
	automountToken := false
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "app", Labels: map[string]string{"app": app}},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: &automountToken,
			Containers:                   []corev1.Container{{Name: "app", Image: "example.invalid/app:1"}},
		},
	}
	if err := c.Create(ctx, pod); err != nil {
		t.Fatal(err)
	}
	return pod
}

func eventsWithReason(t *testing.T, ctx context.Context, c client.Client, namespace, reason string) []corev1.Event {
	t.Helper()
	list := &corev1.EventList{}
	if err := c.List(ctx, list, client.InNamespace(namespace)); err != nil {
		t.Fatal(err)
	}
	var events []corev1.Event
	for _, event := range list.Items {
		if event.Reason == reason {
			events = append(events, event)
		}
	}
	return events
}

func eventually(t *testing.T, ctx context.Context, what string, condition func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for {
		done, state := condition()
		if done {
			return
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			t.Fatalf("timed out waiting for %s (last: %s)", what, state)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// unrelatedCABundle stands in for the CA of a TLS-intercepting proxy.
func unrelatedCABundle(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "intercepting-proxy-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
