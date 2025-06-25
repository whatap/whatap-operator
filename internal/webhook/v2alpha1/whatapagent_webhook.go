/*
Copyright 2025 whatapK8s.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v2alpha1

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
)

// nolint:unused
// log is for logging in this package.
var whatapWebhookLogger = logf.Log.WithName("whatap-webhook")

// SetupWhatapAgentWebhookWithManager registers the webhook for WhatapAgent in the manager.
func SetupWhatapAgentWebhookWithManager(mgr ctrl.Manager) error {
	// Register the Pod webhook for injection
	if err := ctrl.NewWebhookManagedBy(mgr).
		For(&corev1.Pod{}).
		WithDefaulter(&WhatapAgentCustomDefaulter{mgr.GetClient()}).
		WithDefaulterCustomPath("/whatap-injection--v1-pod").
		Complete(); err != nil {
		return err
	}

	// Register the WhatapAgent webhook for validation
	return ctrl.NewWebhookManagedBy(mgr).
		For(&monitoringv2alpha1.WhatapAgent{}).
		WithValidator(&WhatapAgentCustomValidator{client: mgr.GetClient()}).
		WithValidatorCustomPath("/whatap-validation--v2alpha1-whatapagent").
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

type WhatapAgentCustomDefaulter struct {
	client client.Client // webhook 에 등록된 mgr.GetClient()
}

var _ webhook.CustomDefaulter = &WhatapAgentCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind WhatapAgent.
func (d *WhatapAgentCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		whatapWebhookLogger.Info("skipping non-Pod object")
		return nil
	}
	// WhatapAgent CR 가져오기 (클러스터 스코프)
	var whatapAgentCustomResource monitoringv2alpha1.WhatapAgent

	if err := d.client.Get(ctx, client.ObjectKey{Name: "whatap"}, &whatapAgentCustomResource); err != nil {
		// CR이 아직 생성 안 됐으면 주입 안 함
		return nil
	}
	defaultNS := os.Getenv("WHATAP_DEFAULT_NAMESPACE")
	if defaultNS == "" {
		defaultNS = "whatap-monitoring"
	}
	ns := whatapAgentCustomResource.Spec.Features.K8sAgent.Namespace
	if ns == "" {
		ns = defaultNS
	}

	// Check if APM instrumentation is enabled before processing targets
	// Handle the case where instrumentation field might be omitted (zero value)
	instrumentation := whatapAgentCustomResource.Spec.Features.Apm.Instrumentation
	if !instrumentation.Enabled {
		return nil
	}

	// Additional safety check: if targets slice is nil or empty, nothing to process
	if len(instrumentation.Targets) == 0 {
		return nil
	}

	for _, target := range instrumentation.Targets {
		if !target.Enabled {
			continue
		}

		// Check if pod labels match the PodSelector
		if !matchesSelector(pod.Labels, target.PodSelector) {
			continue
		}

		// Get the namespace object to check its labels
		var namespace corev1.Namespace
		if err := d.client.Get(ctx, client.ObjectKey{Name: pod.Namespace}, &namespace); err != nil {
			whatapWebhookLogger.Error(err, "Failed to get namespace", "namespace", pod.Namespace)
			continue
		}

		// Check if namespace matches the NamespaceSelector
		if !matchesNamespaceSelector(pod.Namespace, namespace.Labels, target.NamespaceSelector) {
			continue
		}

		// 4) PodSpec 변형 (initContainer, volumes, env 등)
		patchPodTemplateSpec(&pod.Spec, whatapAgentCustomResource, target, whatapWebhookLogger)
		if pod.Annotations == nil {
			pod.Annotations = map[string]string{}
		}
		// 어노테이션 추가
		if pod.Annotations == nil {
			pod.Annotations = make(map[string]string, 3)
		}
		pod.Annotations["whatap-apm-injected"] = "true"
		pod.Annotations["whatap-apm-language"] = target.Language
		pod.Annotations["whatap-apm-version"] = target.WhatapApmVersions[target.Language]

		// Get pod name or use namespace+generateName if name is empty
		podIdentifier := pod.GetObjectMeta().GetName()
		if podIdentifier == "" {
			// Use namespace + generateName as alternative identifier
			podIdentifier = pod.GetObjectMeta().GetNamespace()
			if pod.GetObjectMeta().GetGenerateName() != "" {
				podIdentifier += "/" + pod.GetObjectMeta().GetGenerateName() + "*"
			} else {
				podIdentifier += "/unknown"
			}
		}

		whatapWebhookLogger.Info("injected Whatap APM into Pod",
			"pod", podIdentifier,
			"language", target.Language,
			"version", target.WhatapApmVersions[target.Language],
		)
		break
	}
	return nil
}

type WhatapAgentCustomValidator struct {
	client client.Client
}

var _ webhook.CustomValidator = &WhatapAgentCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type WhatapAgent.
func (v *WhatapAgentCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	whatapagent, ok := obj.(*monitoringv2alpha1.WhatapAgent)
	if !ok {
		return nil, fmt.Errorf("expected a WhatapAgent object but got %T", obj)
	}
	whatapWebhookLogger.Info("Validation for WhatapAgent upon creation", "name", whatapagent.GetName())

	// Validate required fields
	//if err := validateRequiredFields(whatapagent); err != nil {
	//	return nil, err
	//}

	// Validate APM targets
	//if err := validateApmTargets(whatapagent); err != nil {
	//	return nil, err
	//}

	// Validate agent configurations
	if err := validateAgentConfigurations(whatapagent); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type WhatapAgent.
func (v *WhatapAgentCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	whatapagent, ok := newObj.(*monitoringv2alpha1.WhatapAgent)
	if !ok {
		return nil, fmt.Errorf("expected a WhatapAgent object for the newObj but got %T", newObj)
	}
	whatapWebhookLogger.Info("Validation for WhatapAgent upon update", "name", whatapagent.GetName())

	// Validate required fields
	//if err := validateRequiredFields(whatapagent); err != nil {
	//	return nil, err
	//}

	// Validate APM targets
	//if err := validateApmTargets(whatapagent); err != nil {
	//	return nil, err
	//}

	// Validate agent configurations
	if err := validateAgentConfigurations(whatapagent); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type WhatapAgent.
func (v *WhatapAgentCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	whatapagent, ok := obj.(*monitoringv2alpha1.WhatapAgent)
	if !ok {
		return nil, fmt.Errorf("expected a WhatapAgent object but got %T", obj)
	}
	whatapWebhookLogger.Info("Validation for WhatapAgent upon deletion", "name", whatapagent.GetName())

	// No specific validation for deletion
	return nil, nil
}

// validateRequiredFields checks that all required fields are present and valid
func validateRequiredFields(whatapagent *monitoringv2alpha1.WhatapAgent) error {
	// Check license
	if whatapagent.Spec.License == "" {
		return fmt.Errorf("license is required")
	}

	// Check host
	if whatapagent.Spec.Host == "" {
		return fmt.Errorf("host is required")
	}

	// Check port
	if whatapagent.Spec.Port == "" {
		return fmt.Errorf("port is required")
	}

	return nil
}

// validateApmTargets validates the APM targets configuration
func validateApmTargets(whatapagent *monitoringv2alpha1.WhatapAgent) error {
	for i, target := range whatapagent.Spec.Features.Apm.Instrumentation.Targets {
		// Skip disabled targets
		if !target.Enabled {
			continue
		}

		// Check target name
		if target.Name == "" {
			return fmt.Errorf("target[%d]: name is required", i)
		}

		// Check language
		if target.Language == "" {
			return fmt.Errorf("target[%d]: language is required", i)
		}

		// Check if WhatapApmVersions has an entry for the specified language
		if target.WhatapApmVersions == nil {
			return fmt.Errorf("target[%d]: whatapApmVersions is required", i)
		}
		version, exists := target.WhatapApmVersions[target.Language]
		if !exists || version == "" {
			return fmt.Errorf("target[%d]: whatapApmVersions must include an entry for language '%s'", i, target.Language)
		}

		// Check config mode
		if target.Config.Mode == "custom" {
			if target.Config.ConfigMapRef == nil {
				return fmt.Errorf("target[%d]: configMapRef is required when config mode is 'custom'", i)
			}
			if target.Config.ConfigMapRef.Name == "" {
				return fmt.Errorf("target[%d]: configMapRef.name is required when config mode is 'custom'", i)
			}
			if target.Config.ConfigMapRef.Namespace == "" {
				return fmt.Errorf("target[%d]: configMapRef.namespace is required when config mode is 'custom'", i)
			}
		}
	}

	return nil
}

// validateAgentConfigurations validates the agent configurations
func validateAgentConfigurations(whatapagent *monitoringv2alpha1.WhatapAgent) error {
	// Validate K8sAgent configuration
	whatapAgentCrName := whatapagent.ObjectMeta.Name
	if whatapAgentCrName != "whatap" {
		return fmt.Errorf("agent configuration is not valid")
	}
	return nil
}
