package controller

import (
	"testing"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestNodeAgentUpdateStrategy(t *testing.T) {
	pct := intstr.FromString("10%")
	five := intstr.FromInt32(5)
	zero := intstr.FromInt32(0)

	tests := []struct {
		name            string
		spec            *appsv1.DaemonSetUpdateStrategy
		wantType        appsv1.DaemonSetUpdateStrategyType
		wantUnavailable *intstr.IntOrString
		wantSurge       *intstr.IntOrString
	}{
		{name: "unset keeps maxUnavailable=1", spec: nil, wantType: appsv1.RollingUpdateDaemonSetStrategyType, wantUnavailable: ptrIntstr(intstr.FromInt32(1))},
		{name: "empty type defaults to RollingUpdate", spec: &appsv1.DaemonSetUpdateStrategy{}, wantType: appsv1.RollingUpdateDaemonSetStrategyType, wantUnavailable: ptrIntstr(intstr.FromInt32(1))},
		{name: "percent", spec: &appsv1.DaemonSetUpdateStrategy{RollingUpdate: &appsv1.RollingUpdateDaemonSet{MaxUnavailable: &pct}}, wantType: appsv1.RollingUpdateDaemonSetStrategyType, wantUnavailable: &pct},
		{name: "int", spec: &appsv1.DaemonSetUpdateStrategy{Type: appsv1.RollingUpdateDaemonSetStrategyType, RollingUpdate: &appsv1.RollingUpdateDaemonSet{MaxUnavailable: &five}}, wantType: appsv1.RollingUpdateDaemonSetStrategyType, wantUnavailable: &five},
		{name: "surge only keeps user surge", spec: &appsv1.DaemonSetUpdateStrategy{RollingUpdate: &appsv1.RollingUpdateDaemonSet{MaxUnavailable: &zero, MaxSurge: &pct}}, wantType: appsv1.RollingUpdateDaemonSetStrategyType, wantUnavailable: &zero, wantSurge: &pct},
		{name: "OnDelete drops rollingUpdate", spec: &appsv1.DaemonSetUpdateStrategy{Type: appsv1.OnDeleteDaemonSetStrategyType, RollingUpdate: &appsv1.RollingUpdateDaemonSet{MaxUnavailable: &five}}, wantType: appsv1.OnDeleteDaemonSetStrategyType},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := nodeAgentUpdateStrategy(tc.spec)
			if got.Type != tc.wantType {
				t.Fatalf("type = %q, want %q", got.Type, tc.wantType)
			}
			if tc.wantUnavailable == nil {
				if got.RollingUpdate != nil {
					t.Fatalf("rollingUpdate = %+v, want nil", got.RollingUpdate)
				}
				return
			}
			if got.RollingUpdate == nil || got.RollingUpdate.MaxUnavailable == nil || *got.RollingUpdate.MaxUnavailable != *tc.wantUnavailable {
				t.Fatalf("maxUnavailable = %+v, want %v", got.RollingUpdate, tc.wantUnavailable)
			}
			if (got.RollingUpdate.MaxSurge == nil) != (tc.wantSurge == nil) || (tc.wantSurge != nil && *got.RollingUpdate.MaxSurge != *tc.wantSurge) {
				t.Fatalf("maxSurge = %v, want %v", got.RollingUpdate.MaxSurge, tc.wantSurge)
			}
		})
	}
}

func TestNodeAgentUpdateStrategyDoesNotAliasCR(t *testing.T) {
	pct := intstr.FromString("20%")
	cr := &monitoringv2alpha1.WhatapAgent{}
	cr.Spec.Features.K8sAgent.NodeAgent.UpdateStrategy = &appsv1.DaemonSetUpdateStrategy{RollingUpdate: &appsv1.RollingUpdateDaemonSet{MaxUnavailable: &pct}}

	ds := getNodeAgentDaemonSetSpec("img", &corev1.ResourceRequirements{}, cr, "whatap-node-agent", false)
	if got := ds.UpdateStrategy.RollingUpdate.MaxUnavailable.String(); got != "20%" {
		t.Fatalf("maxUnavailable = %s, want 20%%", got)
	}
	zero := intstr.FromInt32(0)
	ds.UpdateStrategy.RollingUpdate.MaxSurge = &zero
	if cr.Spec.Features.K8sAgent.NodeAgent.UpdateStrategy.RollingUpdate.MaxSurge != nil || cr.Spec.Features.K8sAgent.NodeAgent.UpdateStrategy.Type != "" {
		t.Fatal("generated spec mutated the CR")
	}
}

func ptrIntstr(v intstr.IntOrString) *intstr.IntOrString { return &v }
