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
	return ctrl.NewWebhookManagedBy(mgr).
		For(&corev1.Pod{}).
		//WithValidator(&WhatapAgentCustomValidator{}).
		WithDefaulter(&WhatapAgentCustomDefaulter{mgr.GetClient()}).
		WithDefaulterCustomPath("/whatap-injection--v1-pod").
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
	ns := whatapAgentCustomResource.Spec.Features.KubernetesMonitoring.KubernetesMonitoringNamespace
	if ns == "" {
		ns = defaultNS
	}

	for _, target := range whatapAgentCustomResource.Spec.Features.Apm.Instrumentation.Targets {
		if target.Enabled != "true" {
			continue
		}
		if !hasLabels(pod.Labels, target.PodSelector.MatchLabels) {
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

		whatapWebhookLogger.Info("injected Whatap APM into Pod",
			"pod", pod.Name,
			"language", target.Language,
			"version", target.WhatapApmVersions[target.Language],
		)
		break
	}
	return nil
}

type WhatapAgentCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &WhatapAgentCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type WhatapAgent.
func (v *WhatapAgentCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	whatapagent, ok := obj.(*monitoringv2alpha1.WhatapAgent)
	if !ok {
		return nil, fmt.Errorf("expected a WhatapAgent object but got %T", obj)
	}
	whatapWebhookLogger.Info("Validation for WhatapAgent upon creation", "name", whatapagent.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type WhatapAgent.
func (v *WhatapAgentCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	whatapagent, ok := newObj.(*monitoringv2alpha1.WhatapAgent)
	if !ok {
		return nil, fmt.Errorf("expected a WhatapAgent object for the newObj but got %T", newObj)
	}
	whatapWebhookLogger.Info("Validation for WhatapAgent upon update", "name", whatapagent.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type WhatapAgent.
func (v *WhatapAgentCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	whatapagent, ok := obj.(*monitoringv2alpha1.WhatapAgent)
	if !ok {
		return nil, fmt.Errorf("expected a WhatapAgent object but got %T", obj)
	}
	whatapWebhookLogger.Info("Validation for WhatapAgent upon deletion", "name", whatapagent.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
