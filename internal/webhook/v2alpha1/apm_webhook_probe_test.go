package v2alpha1

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	"github.com/whatap/whatap-operator/internal/apmwebhook"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const probeTestNamespace = "whatap-monitoring"

// x509ProbeError is the API server's response when a TLS-intercepting proxy
// sits between it and the webhook Service.
var x509ProbeError = apierrors.NewInternalError(errors.New(`failed calling webhook "` + apmwebhook.ProbeWebhookName +
	`": failed to call webhook: Post "https://whatap-admission-controller.whatap-monitoring.svc:443/whatap-injection--v1-pod?timeout=10s": ` +
	`tls: failed to verify certificate: x509: certificate signed by unknown authority`))

func TestDefaultMarksOnlyDryRunProbePods(t *testing.T) {
	defaulter := &WhatapAgentCustomDefaulter{client: fake.NewClientBuilder().WithScheme(newProbeTestScheme(t)).Build()}
	dryRun := true
	probeContext := admission.NewContextWithRequest(context.Background(),
		admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{DryRun: &dryRun}})

	probePod := newAPMWebhookProbePod(probeTestNamespace)
	if err := defaulter.Default(probeContext, probePod); err != nil {
		t.Fatalf("Default() error = %v", err)
	}
	if probePod.Annotations[apmwebhook.ProbeReachedAnnotation] != "true" {
		t.Fatal("dry-run probe Pod was not marked as reached")
	}
	if _, evaluated := probePod.Annotations[injectionStatusAnnotation]; evaluated {
		t.Fatal("dry-run probe Pod was evaluated for APM injection")
	}

	labelledPod := newAPMWebhookProbePod("app")
	if err := defaulter.Default(admission.NewContextWithRequest(context.Background(), admission.Request{}), labelledPod); err != nil {
		t.Fatalf("Default() error = %v", err)
	}
	if _, marked := labelledPod.Annotations[apmwebhook.ProbeReachedAnnotation]; marked {
		t.Fatal("a real create request was treated as a probe")
	}
	if labelledPod.Annotations[injectionStatusAnnotation] != injectionStatusSkipped {
		t.Fatal("a real create request carrying the probe label skipped injection evaluation")
	}
}

func TestAPMWebhookProbeRecordsConfirmedOutageAndRecovery(t *testing.T) {
	harness := newProbeHarness(t, apmTestAgent(true))

	harness.respond = harness.reach
	harness.run()
	harness.respond = func(*corev1.Pod) error { return x509ProbeError }
	harness.run()
	if events := harness.events(); len(events) != 0 {
		t.Fatalf("a single failure was reported: %d events", len(events))
	}

	harness.run()
	events := harness.events()
	if len(events) != 1 {
		t.Fatalf("confirmed outage events = %d, want 1", len(events))
	}
	warning := events[0]
	if warning.Type != corev1.EventTypeWarning || warning.Reason != apmWebhookUnreachableReason {
		t.Fatalf("event = %s/%s, want Warning/%s", warning.Type, warning.Reason, apmWebhookUnreachableReason)
	}
	if !strings.Contains(warning.Message, "x509: certificate signed by unknown authority") || !strings.Contains(warning.Message, "NO_PROXY") {
		t.Fatalf("event message lacks the API server's cause or the proxy hint: %q", warning.Message)
	}
	if got := warning.InvolvedObject; got.Kind != "WhatapAgent" || got.Name != "whatap" || got.UID != "agent-uid" ||
		got.APIVersion != "monitoring.whatap.com/v2alpha1" || got.Namespace != "" {
		t.Fatalf("event involvedObject = %#v, want the cluster-scoped WhatapAgent", got)
	}

	harness.advance(apmWebhookProbeInterval)
	harness.run()
	if events := harness.events(); len(events) != 1 {
		t.Fatalf("an unchanged outage was re-recorded within the hour: %d events", len(events))
	}
	harness.advance(apmWebhookEventRepeatInterval)
	harness.run()
	if events := harness.events(); len(events) != 2 {
		t.Fatalf("a lasting outage was not re-recorded after an hour: %d events", len(events))
	}

	harness.respond = func(*corev1.Pod) error {
		return apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("exceeded quota"))
	}
	harness.run()
	if events := harness.events(); len(events) != 2 || !harness.probe.unreachable {
		t.Fatal("an inconclusive probe changed the reported state")
	}

	harness.respond = harness.reach
	harness.run()
	events = harness.events()
	if len(events) != 3 {
		t.Fatalf("events after recovery = %d, want 3", len(events))
	}
	recovered := latestEvent(events)
	if recovered.Type != corev1.EventTypeNormal || recovered.Reason != apmWebhookRecoveredReason {
		t.Fatalf("recovery event = %s/%s", recovered.Type, recovered.Reason)
	}
	harness.run()
	if events := harness.events(); len(events) != 3 {
		t.Fatal("a healthy probe recorded an event")
	}
}

func TestAPMWebhookProbeTreatsAdmissionWithoutWebhookAsUnreachable(t *testing.T) {
	harness := newProbeHarness(t, apmTestAgent(true))
	harness.respond = func(*corev1.Pod) error { return nil }
	harness.run()
	harness.run()
	events := harness.events()
	if len(events) != 1 || events[0].Reason != apmWebhookUnreachableReason ||
		!strings.Contains(events[0].Message, "without calling the webhook") {
		t.Fatalf("events = %#v, want one unreachable event for an unmarked probe", events)
	}
}

func TestAPMWebhookProbeRecordsChangedCauseImmediately(t *testing.T) {
	harness := newProbeHarness(t, apmTestAgent(true))
	harness.respond = func(*corev1.Pod) error { return x509ProbeError }
	harness.run()
	harness.run()
	harness.respond = func(*corev1.Pod) error {
		return apierrors.NewInternalError(errors.New(`failed calling webhook "` + apmwebhook.ProbeWebhookName +
			`": failed to call webhook: Post "https://whatap-admission-controller.whatap-monitoring.svc:443/whatap-injection--v1-pod?timeout=10s": context deadline exceeded`))
	}
	harness.advance(apmWebhookProbeInterval)
	harness.run()
	events := harness.events()
	if len(events) != 2 || !strings.Contains(latestEvent(events).Message, "context deadline exceeded") {
		t.Fatalf("changed cause was not recorded: %#v", events)
	}
	if strings.Contains(latestEvent(events).Message, "NO_PROXY") {
		t.Fatal("proxy hint added to a non-certificate failure")
	}
}

func TestAPMWebhookProbeOnlyRunsWhileInjectionIsConfigured(t *testing.T) {
	disabledTargets := apmTestAgent(true)
	disabledTargets.Spec.Features.Apm.Instrumentation.Targets[0].Enabled = false
	for name, agent := range map[string]*monitoringv2alpha1.WhatapAgent{
		"no WhatapAgent":           nil,
		"instrumentation disabled": apmTestAgent(false),
		"no enabled target":        disabledTargets,
	} {
		t.Run(name, func(t *testing.T) {
			harness := newProbeHarness(t, agent)
			harness.respond = func(*corev1.Pod) error { return x509ProbeError }
			for range apmWebhookProbeFailureThreshold + 1 {
				harness.run()
			}
			if harness.probes != 0 || len(harness.events()) != 0 {
				t.Fatalf("probes = %d, events = %d, want none", harness.probes, len(harness.events()))
			}
		})
	}
}

type probeHarness struct {
	t       *testing.T
	probe   *apmWebhookProbe
	client  client.Client
	clock   time.Time
	probes  int
	respond func(*corev1.Pod) error
}

func newProbeHarness(t *testing.T, agent *monitoringv2alpha1.WhatapAgent) *probeHarness {
	t.Helper()
	harness := &probeHarness{t: t, clock: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)}
	builder := fake.NewClientBuilder().WithScheme(newProbeTestScheme(t)).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			pod, isPod := obj.(*corev1.Pod)
			if !isPod {
				return c.Create(ctx, obj, opts...)
			}
			harness.probes++
			createOptions := &client.CreateOptions{}
			createOptions.ApplyOptions(opts)
			if len(createOptions.DryRun) != 1 || createOptions.DryRun[0] != metav1.DryRunAll {
				t.Fatal("probe Pod was not a dry-run request")
			}
			if pod.Namespace != probeTestNamespace || pod.Labels[apmwebhook.ProbeLabel] != "true" {
				t.Fatalf("probe Pod %s/%s lacks the probe label", pod.Namespace, pod.GenerateName)
			}
			return harness.respond(pod)
		},
	})
	if agent != nil {
		builder = builder.WithObjects(agent)
	}
	harness.client = builder.Build()
	harness.probe = newAPMWebhookProbe(harness.client, probeTestNamespace)
	harness.probe.now = func() time.Time { return harness.clock }
	return harness
}

// reach answers like the API server when it called the webhook.
func (h *probeHarness) reach(pod *corev1.Pod) error {
	pod.Annotations = map[string]string{apmwebhook.ProbeReachedAnnotation: "true"}
	return nil
}

func (h *probeHarness) run() {
	h.probe.Run(context.Background())
	h.advance(time.Second)
}

func (h *probeHarness) advance(d time.Duration) { h.clock = h.clock.Add(d) }

func (h *probeHarness) events() []corev1.Event {
	h.t.Helper()
	return listInjectionFailureEvents(h.t, h.client, metav1.NamespaceDefault)
}

func latestEvent(events []corev1.Event) corev1.Event {
	latest := events[0]
	for _, event := range events[1:] {
		if event.LastTimestamp.After(latest.LastTimestamp.Time) {
			latest = event
		}
	}
	return latest
}

func newProbeTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := monitoringv2alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func apmTestAgent(instrumentationEnabled bool) *monitoringv2alpha1.WhatapAgent {
	agent := &monitoringv2alpha1.WhatapAgent{ObjectMeta: metav1.ObjectMeta{Name: "whatap", UID: "agent-uid"}}
	agent.Spec.Features.Apm.Instrumentation = monitoringv2alpha1.InstrumentationSpec{
		Enabled: instrumentationEnabled,
		Targets: []monitoringv2alpha1.TargetSpec{{
			Name: "demo", Enabled: true, Language: "java",
			PodSelector:       monitoringv2alpha1.PodSelector{MatchLabels: map[string]string{"app": "demo"}},
			NamespaceSelector: monitoringv2alpha1.NamespaceSelector{MatchNames: []string{"app"}},
		}},
	}
	return agent
}
