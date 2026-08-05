package controller

import (
	"testing"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mtlsEndpoint builds an endpoint whose TLS material lives in the given Secret.
func mtlsEndpoint(secretName string) monitoringv2alpha1.OpenAgentEndpoint {
	return monitoringv2alpha1.OpenAgentEndpoint{
		Address: "10.0.0.1:2379",
		Scheme:  "https",
		Path:    "/metrics",
		TLSConfig: &monitoringv2alpha1.TLSConfig{
			InsecureSkipVerify: true,
			CASecret:           &monitoringv2alpha1.SecretKeySelector{Name: secretName, Key: "ca.crt"},
			CertSecret:         &monitoringv2alpha1.SecretKeySelector{Name: secretName, Key: "client.crt"},
			KeySecret:          &monitoringv2alpha1.SecretKeySelector{Name: secretName, Key: "client.key"},
		},
	}
}

// TestCollectAllTLSSecrets_SeparateCRs verifies that TLS Secrets referenced from every
// separate CR kind get collected. generateScrapeConfig rewrites caSecret/certSecret/keySecret
// into /etc/ssl/certs/<secret>/<key> paths for all of them, so a kind missing here produces
// a scrape_config pointing at files that are never mounted (silent scrape failure).
func TestCollectAllTLSSecrets_SeparateCRs(t *testing.T) {
	inlineTargets := []monitoringv2alpha1.OpenAgentTargetSpec{
		{
			TargetName: "inline",
			Type:       "StaticEndpoints",
			Enabled:    true,
			Endpoints:  []monitoringv2alpha1.OpenAgentEndpoint{mtlsEndpoint("inline-cert")},
		},
	}
	podMonitors := &monitoringv2alpha1.WhatapPodMonitorList{
		Items: []monitoringv2alpha1.WhatapPodMonitor{{
			ObjectMeta: metav1.ObjectMeta{Name: "pm", Namespace: "ns"},
			Spec: monitoringv2alpha1.WhatapPodMonitorSpec{
				Endpoints: []monitoringv2alpha1.OpenAgentEndpoint{mtlsEndpoint("podmonitor-cert")},
			},
		}},
	}
	serviceMonitors := &monitoringv2alpha1.WhatapServiceMonitorList{
		Items: []monitoringv2alpha1.WhatapServiceMonitor{{
			ObjectMeta: metav1.ObjectMeta{Name: "sm", Namespace: "ns"},
			Spec: monitoringv2alpha1.WhatapServiceMonitorSpec{
				Endpoints: []monitoringv2alpha1.OpenAgentEndpoint{mtlsEndpoint("servicemonitor-cert")},
			},
		}},
	}
	staticEndpoints := &monitoringv2alpha1.WhatapStaticEndpointList{
		Items: []monitoringv2alpha1.WhatapStaticEndpoint{{
			ObjectMeta: metav1.ObjectMeta{Name: "etcd", Namespace: "whatap-monitoring"},
			Spec: monitoringv2alpha1.WhatapStaticEndpointSpec{
				Endpoints: []monitoringv2alpha1.OpenAgentEndpoint{mtlsEndpoint("etcd-client-cert")},
			},
		}},
	}

	secrets := collectAllTLSSecrets(inlineTargets, podMonitors, serviceMonitors, staticEndpoints)

	for _, name := range []string{"inline-cert", "podmonitor-cert", "servicemonitor-cert", "etcd-client-cert"} {
		keys, ok := secrets[name]
		if !ok {
			t.Errorf("Secret %q not collected; its cert files would never be mounted", name)
			continue
		}
		if len(keys) != 3 {
			t.Errorf("Secret %q: expected 3 keys (ca/cert/key), got %d: %v", name, len(keys), keys)
		}
	}
}

// TestCollectAllTLSSecrets_NilLists guards the operator against panicking when a caller
// passes nil lists (e.g. an API group not present in the cluster).
func TestCollectAllTLSSecrets_NilLists(t *testing.T) {
	secrets := collectAllTLSSecrets(nil, nil, nil, nil)
	if len(secrets) != 0 {
		t.Errorf("expected no secrets, got %v", secrets)
	}
}
