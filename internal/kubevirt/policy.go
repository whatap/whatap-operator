// Package kubevirt contains opt-in first-boot provisioning contracts. It does not
// import a KubeVirt client or start watches on optional KubeVirt CRDs.
package kubevirt

import (
	"fmt"
	"net"
	"strings"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	"github.com/whatap/whatap-operator/internal/kubevirt/cloudinit"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	OptInLabel                   = "monitoring.whatap.com/server-agent"
	InjectedAnnotation           = "monitoring.whatap.com/server-agent-bootstrap"
	TargetAnnotation             = "monitoring.whatap.com/server-agent-target"
	WebhookName                  = "kubevirt-server.monitoring.whatap.com"
	WebhookPath                  = "/whatap-injection-kubevirt-v1-virtualmachine"
	DefaultPort            int32 = 6600
	maxInlineLicenseLength       = 8192
)

// ValidatePolicy is also called at VM admission: the existing WhatapAgent
// validation webhook can be unavailable, and schema validation is not sufficient.
func ValidatePolicy(policy monitoringv2alpha1.KubeVirtSpec) error {
	if !policy.Enabled {
		return nil
	}
	names := make(map[string]bool)
	for i, target := range policy.Targets {
		if !target.Enabled {
			continue
		}
		prefix := fmt.Sprintf("kubeVirt.targets[%d]", i)
		if len(validation.IsDNS1123Label(target.Name)) != 0 || names[target.Name] {
			return fmt.Errorf("%s: name must be a unique DNS label", prefix)
		}
		names[target.Name] = true
		if len(validation.IsDNS1123Label(target.Namespace)) != 0 {
			return fmt.Errorf("%s: an exact namespace is required", prefix)
		}
		if _, err := metav1.LabelSelectorAsSelector(&target.VMSelector); err != nil {
			return fmt.Errorf("%s: invalid VM selector", prefix)
		}
		hasInlineLicense := target.License != ""
		hasSecretRef := target.LicenseSecretRef != nil
		if hasInlineLicense == hasSecretRef {
			return fmt.Errorf("%s: specify exactly one of license or licenseSecretRef", prefix)
		}
		if hasInlineLicense {
			if strings.TrimSpace(target.License) == "" || len(target.License) > maxInlineLicenseLength {
				return fmt.Errorf("%s: invalid inline license", prefix)
			}
			for _, char := range target.License {
				if char < 32 || char > 126 {
					return fmt.Errorf("%s: inline license must be single-line printable ASCII", prefix)
				}
			}
		} else {
			ref := target.LicenseSecretRef
			if len(validation.IsDNS1123Subdomain(ref.Name)) != 0 || len(validation.IsConfigMapKey(ref.Key)) != 0 || ref.Key == "" || (ref.Optional != nil && *ref.Optional) {
				return fmt.Errorf("%s: a non-optional license Secret name/key is required", prefix)
			}
		}
		if target.Port < 0 || target.Port > 65535 {
			return fmt.Errorf("%s: port must be between 1 and 65535", prefix)
		}
		if !cloudinit.ValidAgentVersion(target.AgentVersion) {
			return fmt.Errorf("%s: agentVersion must be a numeric X.Y.Z release (maximum 32 characters)", prefix)
		}
		for _, host := range strings.Split(target.Host, "/") {
			if net.ParseIP(host) == nil && len(validation.IsDNS1123Subdomain(host)) != 0 {
				return fmt.Errorf("%s: host must contain collector IPs or DNS names separated by '/'", prefix)
			}
		}
	}
	return nil
}

// SelectTarget never broadens a namespace or silently chooses between overlapping
// targets. An empty VM selector is safe only because an explicit VM opt-in label
// and an exact target namespace are both mandatory.
func SelectTarget(policy monitoringv2alpha1.KubeVirtSpec, namespace string, vmLabels map[string]string) (*monitoringv2alpha1.KubeVirtTargetSpec, error) {
	if !policy.Enabled || vmLabels[OptInLabel] != "true" {
		return nil, nil
	}
	if err := ValidatePolicy(policy); err != nil {
		return nil, err
	}
	var matched *monitoringv2alpha1.KubeVirtTargetSpec
	for i := range policy.Targets {
		target := &policy.Targets[i]
		if !target.Enabled || target.Namespace != namespace {
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(&target.VMSelector)
		if err != nil {
			return nil, fmt.Errorf("invalid VM selector")
		}
		if !selector.Matches(labels.Set(vmLabels)) {
			continue
		}
		if matched != nil {
			return nil, fmt.Errorf("multiple KubeVirt targets match this VM; selectors must be disjoint")
		}
		matched = target.DeepCopy()
		if matched.Port == 0 {
			matched.Port = DefaultPort
		}
		if matched.AgentVersion == "" {
			matched.AgentVersion = cloudinit.DefaultAgentVersion
		}
	}
	return matched, nil
}
