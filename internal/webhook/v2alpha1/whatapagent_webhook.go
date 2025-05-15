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
		//WithValidator(&WhatapAgentCustomValidator{}).
		WithDefaulter(&WhatapAgentCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// Carefully check out the following marker comments
//+kubebuilder:webhook:path=/mutate--v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=create;update,versions=v1,name=mpod.kb.io,admissionReviewVersions=v1
//+kubebuilder:webhook:path=/validate--v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=create;update;delete,versions=v1,name=vpod.kb.io,admissionReviewVersions=v1

type WhatapAgentCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &WhatapAgentCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind WhatapAgent.
func (d *WhatapAgentCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return fmt.Errorf("expected a Pod but got a %T", obj)
	}

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	// TODO(user): fill in your defaulting logic.
	pod.Annotations["mutating-admission-webhook"] = "whatap"
	whatapagentlog.Info("Annotated Pod", "name", pod.Name)
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
