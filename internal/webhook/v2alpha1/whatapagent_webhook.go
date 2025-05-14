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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
)

// nolint:unused
// log is for logging in this package.
var whatapagentlog = logf.Log.WithName("whatapagent-resource")

// SetupWhatapAgentWebhookWithManager registers the webhook for WhatapAgent in the manager.
func SetupWhatapAgentWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&corev1.Pod{}).
		WithValidator(&WhatapAgentCustomValidator{}).
		WithDefaulter(&WhatapAgentCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-monitoring-whatap-com-v2alpha1-whatapagent,mutating=true,failurePolicy=fail,sideEffects=None,groups=monitoring.whatap.com,resources=whatapagents,verbs=create;update,versions=v2alpha1,name=mwhatapagent-v2alpha1.kb.io,admissionReviewVersions=v1

// WhatapAgentCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind WhatapAgent when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type WhatapAgentCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &WhatapAgentCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind WhatapAgent.
func (d *WhatapAgentCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	whatapagent, ok := obj.(*monitoringv2alpha1.WhatapAgent)

	if !ok {
		return fmt.Errorf("expected an WhatapAgent object but got %T", obj)
	}

	whatapagentlog.Info("Defaulting for WhatapAgent", "name", whatapagent.GetName())
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return fmt.Errorf("expected a Pod but got a %T", obj)
	}

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	// TODO(user): fill in your defaulting logic.
	pod.Annotations["mutating-admission-webhook"] = "whatap"
	whatapagentlog.Info("Annotated Pod", "name", whatapagent.GetName())
	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-monitoring-whatap-com-v2alpha1-whatapagent,mutating=false,failurePolicy=fail,sideEffects=None,groups=monitoring.whatap.com,resources=whatapagents,verbs=create;update,versions=v2alpha1,name=vwhatapagent-v2alpha1.kb.io,admissionReviewVersions=v1

// WhatapAgentCustomValidator struct is responsible for validating the WhatapAgent resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
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
	whatapagentlog.Info("Validation for WhatapAgent upon creation", "name", whatapagent.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type WhatapAgent.
func (v *WhatapAgentCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	whatapagent, ok := newObj.(*monitoringv2alpha1.WhatapAgent)
	if !ok {
		return nil, fmt.Errorf("expected a WhatapAgent object for the newObj but got %T", newObj)
	}
	whatapagentlog.Info("Validation for WhatapAgent upon update", "name", whatapagent.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type WhatapAgent.
func (v *WhatapAgentCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	whatapagent, ok := obj.(*monitoringv2alpha1.WhatapAgent)
	if !ok {
		return nil, fmt.Errorf("expected a WhatapAgent object but got %T", obj)
	}
	whatapagentlog.Info("Validation for WhatapAgent upon deletion", "name", whatapagent.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
