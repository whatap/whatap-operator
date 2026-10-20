package v2alpha1

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	"github.com/whatap/whatap-operator/internal/apmwebhook"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	whatapAgentName = "whatap"

	// apmWebhookProbeInitialDelay gives the reconciler time to publish the
	// webhook CA that the operator generates at startup.
	apmWebhookProbeInitialDelay = time.Minute
	apmWebhookProbeInterval     = 5 * time.Minute
	apmWebhookProbeTimeout      = 30 * time.Second
	// apmWebhookProbeFailureThreshold consecutive failures are reported, so the
	// CA and endpoint changes of an operator restart are not.
	apmWebhookProbeFailureThreshold = 2
	// apmWebhookEventRepeatInterval re-records a lasting outage about once per
	// default Event TTL so that it stays visible on the WhatapAgent.
	apmWebhookEventRepeatInterval = time.Hour
	apmWebhookCauseMaxLength      = 512
	// The probe Pod is a dry-run request and is never scheduled or pulled.
	apmWebhookProbeImage = "registry.k8s.io/pause:3.10"

	apmWebhookUnreachableReason = "APMInjectionWebhookUnreachable"
	apmWebhookRecoveredReason   = "APMInjectionWebhookRecovered"
	apmWebhookRecoveredMessage  = "kube-apiserver can call the WhaTap APM injection webhook again. " +
		"Restart Pods that have " + apmInjectionMissingReason + " events to inject APM agents."
)

// +kubebuilder:rbac:groups="",resources=pods,verbs=create

// SetupAPMWebhookDiagnosticsWithManager reports APM injection webhook calls that
// the API server could not make: a reachability probe recorded on the
// WhatapAgent, and a Warning Event on every Pod admitted without injection
// because of it. namespace is the operator namespace, where the dry-run probe
// Pods are created.
func SetupAPMWebhookDiagnosticsWithManager(mgr ctrl.Manager, namespace string) error {
	startedAt := time.Now()
	if err := (&MissedInjectionEventReconciler{Client: mgr.GetClient(), NotBefore: startedAt}).SetupWithManager(mgr); err != nil {
		return err
	}
	return mgr.Add(newAPMWebhookProbe(mgr.GetClient(), namespace))
}

// isAPMWebhookProbe reports whether pod is a dry-run reachability probe. Pods
// that carry the label in a real create request are processed normally.
func isAPMWebhookProbe(ctx context.Context, pod *corev1.Pod) bool {
	if pod.Labels[apmwebhook.ProbeLabel] != "true" {
		return false
	}
	request, err := admission.RequestFromContext(ctx)
	return err == nil && request.DryRun != nil && *request.DryRun
}

// apmInjectionConfigured reports whether the injection webhook would inject
// any Pod, i.e. instrumentation is on and at least one target is enabled.
func apmInjectionConfigured(agent *monitoringv2alpha1.WhatapAgent) bool {
	instrumentation := agent.Spec.Features.Apm.Instrumentation
	if !instrumentation.Enabled {
		return false
	}
	for _, target := range instrumentation.Targets {
		if target.Enabled {
			return true
		}
	}
	return false
}

type apmWebhookProbeOutcome int

const (
	apmWebhookReached apmWebhookProbeOutcome = iota
	apmWebhookUnreachable
	// apmWebhookProbeInconclusive means the probe Pod was rejected for another
	// reason (RBAC, quota, a policy engine), so reachability is unknown.
	apmWebhookProbeInconclusive
)

// apmWebhookProbe checks, from the API server's side, that new Pods reach the
// APM injection webhook. Every interval it dry-run creates a probe Pod in the
// operator namespace. After apmWebhookProbeFailureThreshold consecutive
// failures it logs the API server's error and records a Warning Event on the
// WhatapAgent, and it records a Normal Event once the webhook is reachable
// again. It probes only while APM injection is configured.
type apmWebhookProbe struct {
	client       client.Client
	namespace    string
	initialDelay time.Duration
	interval     time.Duration
	now          func() time.Time
	log          logr.Logger

	failures         int
	unreachable      bool
	lastCause        string
	lastEventAt      time.Time
	lastInconclusive string
}

func newAPMWebhookProbe(c client.Client, namespace string) *apmWebhookProbe {
	return &apmWebhookProbe{
		client:       c,
		namespace:    namespace,
		initialDelay: apmWebhookProbeInitialDelay,
		interval:     apmWebhookProbeInterval,
		now:          time.Now,
		log:          whatapWebhookLogger.WithName("apm-webhook-probe"),
	}
}

// NeedLeaderElection keeps the probe, and its Events, on a single replica.
func (p *apmWebhookProbe) NeedLeaderElection() bool { return true }

// Start probes after initialDelay and then every interval until ctx is done.
func (p *apmWebhookProbe) Start(ctx context.Context) error {
	timer := time.NewTimer(p.initialDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		p.Run(ctx)
		timer.Reset(p.interval)
	}
}

// Run performs one probe and records any change in reachability.
func (p *apmWebhookProbe) Run(ctx context.Context) {
	agent := &monitoringv2alpha1.WhatapAgent{}
	if err := p.client.Get(ctx, client.ObjectKey{Name: whatapAgentName}, agent); err != nil {
		if !apierrors.IsNotFound(err) {
			p.log.Error(err, "Failed to read WhatapAgent CR; skipping the APM injection webhook probe")
		}
		return
	}
	if !apmInjectionConfigured(agent) {
		p.failures, p.unreachable, p.lastCause = 0, false, ""
		return
	}
	outcome, detail := p.probe(ctx)
	p.observe(ctx, agent, outcome, detail)
}

// probe dry-run creates a probe Pod. Both mpod.kb.io and the fail-closed
// probe webhook select it, so a reachable webhook always marks it, and an
// unreachable one makes the API server return the call error.
func (p *apmWebhookProbe) probe(ctx context.Context) (apmWebhookProbeOutcome, string) {
	ctx, cancel := context.WithTimeout(ctx, apmWebhookProbeTimeout)
	defer cancel()
	pod := newAPMWebhookProbePod(p.namespace)
	err := p.client.Create(ctx, pod, client.DryRunAll)
	switch {
	case err == nil && pod.Annotations[apmwebhook.ProbeReachedAnnotation] == "true":
		return apmWebhookReached, ""
	case err == nil:
		return apmWebhookUnreachable, "kube-apiserver admitted the probe Pod without calling the webhook " +
			"(the MutatingWebhookConfiguration is missing or does not include " + apmwebhook.ProbeWebhookName + ")"
	case strings.Contains(err.Error(), fmt.Sprintf("failed calling webhook %q", apmwebhook.ProbeWebhookName)):
		return apmWebhookUnreachable, err.Error()
	default:
		return apmWebhookProbeInconclusive, err.Error()
	}
}

func (p *apmWebhookProbe) observe(ctx context.Context, agent *monitoringv2alpha1.WhatapAgent, outcome apmWebhookProbeOutcome, detail string) {
	switch outcome {
	case apmWebhookReached:
		p.lastInconclusive = ""
		if p.unreachable {
			p.log.Info("kube-apiserver can call the APM injection webhook again")
			p.record(ctx, agent, corev1.EventTypeNormal, apmWebhookRecoveredReason, apmWebhookRecoveredMessage)
		}
		p.failures, p.unreachable, p.lastCause = 0, false, ""
	case apmWebhookUnreachable:
		p.lastInconclusive = ""
		p.failures++
		if p.failures < apmWebhookProbeFailureThreshold {
			p.log.Info("APM injection webhook probe failed; confirming on the next probe", "cause", detail)
			return
		}
		p.log.Error(errors.New(detail), "kube-apiserver cannot call the APM injection webhook; new Pods are created without APM injection")
		now := p.now()
		if !p.unreachable || detail != p.lastCause || now.Sub(p.lastEventAt) >= apmWebhookEventRepeatInterval {
			if p.record(ctx, agent, corev1.EventTypeWarning, apmWebhookUnreachableReason, apmWebhookUnreachableMessage(detail)) {
				p.lastEventAt = now
			}
		}
		p.unreachable, p.lastCause = true, detail
	case apmWebhookProbeInconclusive:
		if detail != p.lastInconclusive {
			p.log.Info("Could not check whether kube-apiserver can call the APM injection webhook", "error", detail)
			p.lastInconclusive = detail
		}
	}
}

// record creates an Event on the cluster-scoped WhatapAgent. Such Events live
// in the default namespace, and the WhaTap Kubernetes agent forwards them to
// the cluster project.
func (p *apmWebhookProbe) record(ctx context.Context, agent *monitoringv2alpha1.WhatapAgent, eventType, reason, message string) bool {
	now := metav1.NewTime(p.now())
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s.%x", agent.Name, now.UnixNano()),
			Namespace: metav1.NamespaceDefault,
		},
		InvolvedObject: corev1.ObjectReference{
			APIVersion: monitoringv2alpha1.GroupVersion.String(),
			Kind:       "WhatapAgent",
			Name:       agent.Name,
			UID:        agent.UID,
		},
		Reason:         reason,
		Message:        message,
		Source:         corev1.EventSource{Component: injectionFailureEventComponent},
		FirstTimestamp: now,
		LastTimestamp:  now,
		Count:          1,
		Type:           eventType,
	}
	if err := p.client.Create(ctx, event); err != nil {
		p.log.Error(err, "Failed to record the APM injection webhook Event", "reason", reason)
		return false
	}
	return true
}

func apmWebhookUnreachableMessage(cause string) string {
	if len(cause) > apmWebhookCauseMaxLength {
		cause = strings.ToValidUTF8(cause[:apmWebhookCauseMaxLength], "") + "..."
	}
	message := "kube-apiserver cannot call the WhaTap APM injection webhook, so new Pods are created without APM agents. Cause: " + cause
	if strings.Contains(cause, "x509") {
		message += ". If an HTTPS proxy intercepts the connections of kube-apiserver, add .svc to the NO_PROXY of kube-apiserver."
	}
	return message
}

// newAPMWebhookProbePod returns a minimal Pod that satisfies the restricted Pod
// Security Standard, so admission policies in the operator namespace rarely
// reject it. It sets no runAsUser, leaving OpenShift SCCs free to assign one.
func newAPMWebhookProbePod(namespace string) *corev1.Pod {
	runAsNonRoot, allowPrivilegeEscalation, readOnlyRootFilesystem, automountToken := true, false, true, false
	resources := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("10m"),
		corev1.ResourceMemory: resource.MustParse("16Mi"),
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "whatap-apm-webhook-probe-",
			Namespace:    namespace,
			Labels:       map[string]string{apmwebhook.ProbeLabel: "true"},
		},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: &automountToken,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   &runAsNonRoot,
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []corev1.Container{{
				Name:      "probe",
				Image:     apmWebhookProbeImage,
				Resources: corev1.ResourceRequirements{Requests: resources, Limits: resources},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: &allowPrivilegeEscalation,
					ReadOnlyRootFilesystem:   &readOnlyRootFilesystem,
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
			}},
		},
	}
}
