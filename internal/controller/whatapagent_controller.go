package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	"github.com/whatap/whatap-operator/internal/config"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	webhookServiceName       = "whatap-admission-controller"
	webhookSecretName        = "whatap-webhook-certificate"
	webhookConfigurationName = "whatap-webhook"
	whatapFinalizer          = "whatapagent.finalizers.monitoring.whatap.com"
)

// WhatapAgentReconciler reconciles a WhatapAgent object
type WhatapAgentReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Recorder         record.EventRecorder
	DefaultNamespace string
	// from main.go
	WebhookCABundle []byte
	CaKey           []byte
	ServerCert      []byte
	ServerKey       []byte
}

func (r *WhatapAgentReconciler) ensureWebhookTLSSecret(ctx context.Context, whatapAgent *monitoringv2alpha1.WhatapAgent) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      webhookSecretName,
			Namespace: r.DefaultNamespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		// Set WhatapAgent instance as the owner and controller
		if err := controllerutil.SetControllerReference(whatapAgent, secret, r.Scheme); err != nil {
			return err
		}
		secret.Data = map[string][]byte{
			"cert.pem": r.WebhookCABundle, // CA 번들
			"key.pem":  r.CaKey,
			"tls.crt":  r.ServerCert,
			"tls.key":  r.ServerKey,
		}
		return nil
	})
	return err
}

func (r *WhatapAgentReconciler) cleanupMasterAgent(ctx context.Context) error {
	logger := log.FromContext(ctx)
	if err := r.Delete(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-master-agent", Namespace: r.DefaultNamespace},
	}); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete Master Agent Deployment")
			return err
		}
	}
	return nil
}

func (r *WhatapAgentReconciler) cleanupNodeAgent(ctx context.Context) error {
	logger := log.FromContext(ctx)
	if err := r.Delete(ctx, &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-node-agent", Namespace: r.DefaultNamespace},
	}); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete Node Agent DaemonSet")
			return err
		}
	}
	return nil
}

func (r *WhatapAgentReconciler) cleanupOpenAgent(ctx context.Context) error {
	logger := log.FromContext(ctx)
	// Delete OpenAgent Deployment
	if err := r.Delete(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-open-agent", Namespace: r.DefaultNamespace},
	}); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete OpenAgent Deployment")
			return err
		}
	}

	// Delete OpenAgent ConfigMap
	if err := r.Delete(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-open-agent-config", Namespace: r.DefaultNamespace},
	}); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete OpenAgent ConfigMap")
			return err
		}
	}

	// Delete OpenAgent ServiceAccount
	if err := r.Delete(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-open-agent-sa", Namespace: r.DefaultNamespace},
	}); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete OpenAgent ServiceAccount")
			return err
		}
	}

	// Delete OpenAgent ClusterRole
	if err := r.Delete(ctx, &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-open-agent-role"},
	}); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete OpenAgent ClusterRole")
			return err
		}
	}

	// Delete OpenAgent ClusterRoleBinding
	if err := r.Delete(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-open-agent-role-binding"},
	}); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete OpenAgent ClusterRoleBinding")
			return err
		}
	}
	return nil
}

func (r *WhatapAgentReconciler) cleanupAgents(ctx context.Context) error {
	logger := log.FromContext(ctx)
	logger.Info("Cleaning up Whatap agents and resources")

	// Delete Master Agent
	if err := r.cleanupMasterAgent(ctx); err != nil {
		// Logged in helper
	}

	// Delete Node Agent
	if err := r.cleanupNodeAgent(ctx); err != nil {
		// Logged in helper
	}

	// Delete OpenAgent resources
	if err := r.cleanupOpenAgent(ctx); err != nil {
		// Logged in helper
	}

	// Delete MutatingWebhookConfiguration
	if err := r.Delete(ctx, &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: webhookConfigurationName},
	}); err != nil {
		// Ignore NotFound errors
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete MutatingWebhookConfiguration")
		}
	}

	// Delete Webhook Service
	if err := r.Delete(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: webhookServiceName, Namespace: r.DefaultNamespace},
	}); err != nil {
		// Ignore NotFound errors
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete Webhook Service")
		}
	}

	// Delete Webhook Secret
	if err := r.Delete(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSecretName, Namespace: r.DefaultNamespace},
	}); err != nil {
		// Ignore NotFound errors
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete Webhook Secret")
		}
	}

	logger.Info("Cleanup completed")
	return nil
}

func (r *WhatapAgentReconciler) ensureWebhookService(ctx context.Context, whatapAgent *monitoringv2alpha1.WhatapAgent) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      webhookServiceName,
			Namespace: r.DefaultNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "whatap-operator",
				"app.kubernetes.io/managed-by": "whatap-operator",
			},
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		// Set WhatapAgent instance as the owner and controller
		if err := controllerutil.SetControllerReference(whatapAgent, svc, r.Scheme); err != nil {
			return err
		}
		// Apply labels
		if svc.Labels == nil {
			svc.Labels = make(map[string]string)
		}
		svc.Labels["app.kubernetes.io/name"] = "whatap-operator"
		svc.Labels["app.kubernetes.io/managed-by"] = "whatap-operator"

		svc.Spec = corev1.ServiceSpec{
			Selector: map[string]string{
				"app.kubernetes.io/name": "whatap-operator",
			},
			Ports: []corev1.ServicePort{{
				Port: 443,

				TargetPort: intstr.FromInt32(9443),
				Protocol:   corev1.ProtocolTCP,
			}},
		}
		return nil
	})
	return err
}
func (r *WhatapAgentReconciler) ensureMutatingWebhookConfiguration(ctx context.Context, whatapAgent *monitoringv2alpha1.WhatapAgent) error {
	mwc := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookConfigurationName,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, mwc, func() error {
		// Set WhatapAgent instance as the owner and controller
		if err := controllerutil.SetControllerReference(whatapAgent, mwc, r.Scheme); err != nil {
			return err
		}

		// Helper to find existing webhook by name
		// If found, return it (copy). If not, return a new one with Name set.
		findWebhook := func(name string) (admissionregistrationv1.MutatingWebhook, bool) {
			for _, wh := range mwc.Webhooks {
				if wh.Name == name {
					return wh, true
				}
			}
			return admissionregistrationv1.MutatingWebhook{Name: name}, false
		}

		// 1. mpod.kb.io
		mpod, _ := findWebhook("mpod.kb.io")
		mpod.ClientConfig = admissionregistrationv1.WebhookClientConfig{
			Service: &admissionregistrationv1.ServiceReference{
				Name:      webhookServiceName,
				Namespace: r.DefaultNamespace,
				Path:      strPtr("/whatap-injection--v1-pod"),
			},
			CABundle: r.WebhookCABundle,
		}
		mpod.Rules = []admissionregistrationv1.RuleWithOperations{{
			Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
			Rule: admissionregistrationv1.Rule{
				APIGroups:   []string{""},
				APIVersions: []string{"v1"},
				Resources:   []string{"pods"},
			},
		}}
		mpod.FailurePolicy = failurePtr(admissionregistrationv1.Ignore)
		mpod.AdmissionReviewVersions = []string{"v1"}
		mpod.SideEffects = &sideEffectNone

		// 2. whatapagent.kb.io
		whatap, _ := findWebhook("whatapagent.kb.io")
		whatap.ClientConfig = admissionregistrationv1.WebhookClientConfig{
			Service: &admissionregistrationv1.ServiceReference{
				Name:      webhookServiceName,
				Namespace: r.DefaultNamespace,
				Path:      strPtr("/whatap-validation--v2alpha1-whatapagent"),
			},
			CABundle: r.WebhookCABundle,
		}
		whatap.Rules = []admissionregistrationv1.RuleWithOperations{{
			Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update},
			Rule: admissionregistrationv1.Rule{
				APIGroups:   []string{"monitoring.whatap.com"},
				APIVersions: []string{"v2alpha1"},
				Resources:   []string{"whatapagents"},
			},
		}}
		whatap.FailurePolicy = failurePtr(admissionregistrationv1.Ignore)
		whatap.AdmissionReviewVersions = []string{"v1"}
		whatap.SideEffects = &sideEffectNone

		// Assign merged webhooks in stable order
		// By using the structs retrieved from 'mwc.Webhooks', we preserve all other fields
		// (e.g., NamespaceSelector, ObjectSelector, MatchPolicy, TimeoutSeconds)
		// that we did not explicitly overwrite.
		mwc.Webhooks = []admissionregistrationv1.MutatingWebhook{mpod, whatap}
		return nil
	})
	return err
}

func strPtr(s string) *string { return &s }
func failurePtr(p admissionregistrationv1.FailurePolicyType) *admissionregistrationv1.FailurePolicyType {
	return &p
}

var sideEffectNone = admissionregistrationv1.SideEffectClassNone

// populateCredentialsFromEnv logs environment variables usage
func (r *WhatapAgentReconciler) populateCredentialsFromEnv(ctx context.Context, whatapAgent *monitoringv2alpha1.WhatapAgent) error {
	logger := log.FromContext(ctx)

	// Environment variables are now used directly without updating CR
	license := config.GetWhatapLicense()
	if license != "" {
		logger.Info("Using License from environment variable", "license", license)
	}

	host := config.GetWhatapHost()
	if host != "" {
		logger.Info("Using Host from environment variable", "host", host)
	}

	port := config.GetWhatapPort()
	if port != "" {
		logger.Info("Using Port from environment variable", "port", port)
	}

	return nil
}

//+kubebuilder:rbac:groups=monitoring.whatap.com,resources=whatapagents,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=monitoring.whatap.com,resources=whatapagents/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=monitoring.whatap.com,resources=whatapagents/finalizers,verbs=update
//+kubebuilder:rbac:groups=monitoring.whatap.com,resources=whatappodmonitors;whatapservicemonitors,verbs=get;list;watch

// Reconcile
func (r *WhatapAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	whatapAgent := &monitoringv2alpha1.WhatapAgent{}
	if err := r.Get(ctx, req.NamespacedName, whatapAgent); err != nil {
		logger.Error(err, "Failed to get WhatapAgent CR")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Apply finalizer
	if whatapAgent.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(whatapAgent, whatapFinalizer) {
			controllerutil.AddFinalizer(whatapAgent, whatapFinalizer)
			if err := r.Update(ctx, whatapAgent); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if controllerutil.ContainsFinalizer(whatapAgent, whatapFinalizer) {
			// our finalizer is present, so let's handle any external dependency
			if err := r.cleanupAgents(ctx); err != nil {
				logger.Error(err, "Failed to clean up agents")
				// Continue with finalizer removal even if cleanup fails
			}
		}
		// remove our finalizer from the list and update it.
		controllerutil.RemoveFinalizer(whatapAgent, whatapFinalizer)
		if err := r.Update(ctx, whatapAgent); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	logger.Info("Reconciling WhatapAgent", "Name", whatapAgent.Name)

	// Update Status to Progressing
	apimeta.SetStatusCondition(&whatapAgent.Status.Conditions, metav1.Condition{
		Type:    "Available",
		Status:  metav1.ConditionFalse,
		Reason:  "Reconciling",
		Message: "Reconciling WhatapAgent resources",
	})
	// Ignore error on status update to proceed with reconciliation
	_ = r.Status().Update(ctx, whatapAgent)

	// --- 1. create webhook service
	if err := r.ensureWebhookService(ctx, whatapAgent); err != nil {
		logger.Error(err, "failed to ensure ensureWebhookService")
		r.Recorder.Event(whatapAgent, corev1.EventTypeWarning, "InstallFailed", "Failed to ensure Webhook Service: "+err.Error())
		apimeta.SetStatusCondition(&whatapAgent.Status.Conditions, metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionFalse,
			Reason:  "InstallFailed",
			Message: err.Error(),
		})
		r.Status().Update(ctx, whatapAgent)
		return ctrl.Result{}, err
	}

	// --- 2. create webhook secret
	if err := r.ensureWebhookTLSSecret(ctx, whatapAgent); err != nil {
		r.Recorder.Event(whatapAgent, corev1.EventTypeWarning, "InstallFailed", "Failed to ensure Webhook Secret: "+err.Error())
		apimeta.SetStatusCondition(&whatapAgent.Status.Conditions, metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionFalse,
			Reason:  "InstallFailed",
			Message: err.Error(),
		})
		r.Status().Update(ctx, whatapAgent)
		return ctrl.Result{}, err
	}

	// 5) WebhookConfiguration 생성/업데이트
	if err := r.ensureMutatingWebhookConfiguration(ctx, whatapAgent); err != nil {
		r.Recorder.Event(whatapAgent, corev1.EventTypeWarning, "InstallFailed", "Failed to ensure MutatingWebhookConfiguration: "+err.Error())
		apimeta.SetStatusCondition(&whatapAgent.Status.Conditions, metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionFalse,
			Reason:  "InstallFailed",
			Message: err.Error(),
		})
		r.Status().Update(ctx, whatapAgent)
		return ctrl.Result{}, err
	}

	// Kubernetes Monitoring
	k8sAgentSpec := whatapAgent.Spec.Features.K8sAgent
	openAgentSpec := whatapAgent.Spec.Features.OpenAgent
	// GPU ConfigMap is now created by Helm, so we don't need to create it here
	// if k8sAgentSpec.GpuMonitoring.Enabled {
	// 	logger.Info("createOrUpdate Whatap GPU Monitoring ConfigMap/dcgm-exporter-csv")
	// 	if err := createOrUpdateGpuConfigMap(ctx, r, logger, whatapAgent); err != nil {
	// 		logger.Error(err, "Failed to createOrUpdate GPU Monitoring ConfigMap")
	// 	}
	// }

	if k8sAgentSpec.MasterAgent.Enabled {
		logger.V(1).Info("createOrUpdate Whatap Master Agent")
		if err := createOrUpdateMasterAgent(ctx, r, logger, whatapAgent); err != nil {
			logger.Error(err, "Failed to createOrUpdate Master Agent")
			r.Recorder.Event(whatapAgent, corev1.EventTypeWarning, "InstallFailed", "Failed to createOrUpdate Master Agent: "+err.Error())
			apimeta.SetStatusCondition(&whatapAgent.Status.Conditions, metav1.Condition{
				Type:    "Available",
				Status:  metav1.ConditionFalse,
				Reason:  "InstallFailed",
				Message: err.Error(),
			})
			r.Status().Update(ctx, whatapAgent)
			return ctrl.Result{}, err
		}
	} else {
		// Cleanup Master Agent if disabled
		logger.V(1).Info("Cleaning up Whatap Master Agent (disabled)")
		if err := r.cleanupMasterAgent(ctx); err != nil {
			logger.Error(err, "Failed to cleanup Master Agent")
		}
	}
	if k8sAgentSpec.NodeAgent.Enabled {
		logger.V(1).Info("createOrUpdate Whatap Node Agent")
		if err := createOrUpdateNodeAgent(ctx, r, logger, whatapAgent); err != nil {
			logger.Error(err, "Failed to createOrUpdate Node Agent")
			r.Recorder.Event(whatapAgent, corev1.EventTypeWarning, "InstallFailed", "Failed to createOrUpdate Node Agent: "+err.Error())
			apimeta.SetStatusCondition(&whatapAgent.Status.Conditions, metav1.Condition{
				Type:    "Available",
				Status:  metav1.ConditionFalse,
				Reason:  "InstallFailed",
				Message: err.Error(),
			})
			r.Status().Update(ctx, whatapAgent)
			return ctrl.Result{}, err
		}
	} else {
		// Cleanup Node Agent if disabled
		logger.V(1).Info("Cleaning up Whatap Node Agent (disabled)")
		if err := r.cleanupNodeAgent(ctx); err != nil {
			logger.Error(err, "Failed to cleanup Node Agent")
		}
	}
	if k8sAgentSpec.ApiserverMonitoring.Enabled {
		logger.V(1).Info("Installing Apiserver Monitoring Agent")
		if err := installApiserverMonitor(ctx, r, logger, whatapAgent); err != nil {
			logger.Error(err, "Failed to install Apiserver Monitor")
			r.Recorder.Event(whatapAgent, corev1.EventTypeWarning, "InstallFailed", "Failed to install Apiserver Monitor: "+err.Error())
			apimeta.SetStatusCondition(&whatapAgent.Status.Conditions, metav1.Condition{
				Type:    "Available",
				Status:  metav1.ConditionFalse,
				Reason:  "InstallFailed",
				Message: err.Error(),
			})
			r.Status().Update(ctx, whatapAgent)
			return ctrl.Result{}, err
		}
	}
	if k8sAgentSpec.EtcdMonitoring.Enabled {
		logger.V(1).Info("Installing Etcd Monitoring Agent")
		if err := installEtcdMonitor(ctx, r, logger, whatapAgent); err != nil {
			logger.Error(err, "Failed to install Etcd Monitor")
			r.Recorder.Event(whatapAgent, corev1.EventTypeWarning, "InstallFailed", "Failed to install Etcd Monitor: "+err.Error())
			apimeta.SetStatusCondition(&whatapAgent.Status.Conditions, metav1.Condition{
				Type:    "Available",
				Status:  metav1.ConditionFalse,
				Reason:  "InstallFailed",
				Message: err.Error(),
			})
			r.Status().Update(ctx, whatapAgent)
			return ctrl.Result{}, err
		}
	}
	if k8sAgentSpec.SchedulerMonitoring.Enabled {
		logger.V(1).Info("Installing Scheduler Monitoring Agent")
		if err := installSchedulerMonitor(ctx, r, logger, whatapAgent); err != nil {
			logger.Error(err, "Failed to install Scheduler Monitor")
			r.Recorder.Event(whatapAgent, corev1.EventTypeWarning, "InstallFailed", "Failed to install Scheduler Monitor: "+err.Error())
			apimeta.SetStatusCondition(&whatapAgent.Status.Conditions, metav1.Condition{
				Type:    "Available",
				Status:  metav1.ConditionFalse,
				Reason:  "InstallFailed",
				Message: err.Error(),
			})
			r.Status().Update(ctx, whatapAgent)
			return ctrl.Result{}, err
		}
	}
	// OpenAgent
	if openAgentSpec.Enabled {
		logger.V(1).Info("Installing Open Agent")
		if err := installOpenAgent(ctx, r, logger, whatapAgent); err != nil {
			logger.Error(err, "Failed to install Open Agent")
			r.Recorder.Event(whatapAgent, corev1.EventTypeWarning, "InstallFailed", "Failed to install Open Agent: "+err.Error())
			apimeta.SetStatusCondition(&whatapAgent.Status.Conditions, metav1.Condition{
				Type:    "Available",
				Status:  metav1.ConditionFalse,
				Reason:  "InstallFailed",
				Message: err.Error(),
			})
			r.Status().Update(ctx, whatapAgent)
			return ctrl.Result{}, err
		}
	} else {
		// Cleanup Open Agent if disabled
		logger.V(1).Info("Cleaning up Whatap Open Agent (disabled)")
		if err := r.cleanupOpenAgent(ctx); err != nil {
			logger.Error(err, "Failed to cleanup Open Agent")
		}
	}

	// Success
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := r.Get(ctx, req.NamespacedName, whatapAgent); err != nil {
			return err
		}
		apimeta.SetStatusCondition(&whatapAgent.Status.Conditions, metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionTrue,
			Reason:  "Installed",
			Message: "WhatapAgent installed successfully",
		})
		whatapAgent.Status.ObservedGeneration = whatapAgent.Generation
		return r.Status().Update(ctx, whatapAgent)
	})
	if err != nil {
		logger.Error(err, "Failed to update WhatapAgent status")
		return ctrl.Result{}, err
	}
	// Do not emit event on every reconcile loop if it's already available?
	// But Reconcile is periodic. We should only emit if status changed?
	// For simplicity, I'll only emit if I just set it to True.
	// But `SetStatusCondition` handles deduplication.
	// I'll emit "Installed" event. It might spam if we requeue every 5 mins.
	// Better to check if condition changed.
	// apimeta.SetStatusCondition returns bool (if changed).
	// But I just called it.
	// Let's just emit it. `kubectl describe` shows last seen.
	// Actually, if we want to avoid spamming events, we can skip it.
	// But the user asked for improvements.
	// I will just add the status update and return.

	// Schedule periodic reconciliation to ensure resources are maintained
	return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
}

// 헬퍼: 슬라이스에 문자열이 있는지 확인
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// 헬퍼: 슬라이스에서 문자열 제거
func removeString(slice []string, s string) []string {
	result := []string{}
	for _, v := range slice {
		if v != s {
			result = append(result, v)
		}
	}
	return result
}

func (r *WhatapAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	lp := loggingPredicate(mgr.GetLogger().WithName("event-watcher"))
	return ctrl.NewControllerManagedBy(mgr).
		// 1) Watch the cluster-scoped WhatapAgent so CR changes still reconcile
		// Use GenerationChangedPredicate to avoid reconciliation loops on Status updates
		For(&monitoringv2alpha1.WhatapAgent{}, builder.WithPredicates(predicate.GenerationChangedPredicate{}, lp)).
		// Watch for changes to resources created by this controller
		Owns(&appsv1.Deployment{}, builder.WithPredicates(lp)).
		Owns(&appsv1.DaemonSet{}, builder.WithPredicates(lp)).
		Owns(&corev1.Service{}, builder.WithPredicates(lp)).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(lp)).
		Owns(&corev1.Secret{}, builder.WithPredicates(lp)).
		Owns(&corev1.ServiceAccount{}, builder.WithPredicates(lp)).
		Owns(&rbacv1.ClusterRole{}, builder.WithPredicates(lp)).
		Owns(&rbacv1.ClusterRoleBinding{}, builder.WithPredicates(lp)).
		Owns(&admissionregistrationv1.MutatingWebhookConfiguration{}, builder.WithPredicates(lp)).
		// Watch for WhatapPodMonitor
		Watches(
			&monitoringv2alpha1.WhatapPodMonitor{},
			handler.EnqueueRequestsFromMapFunc(r.findWhatapAgents),
			builder.WithPredicates(lp),
		).
		// Watch for WhatapServiceMonitor
		Watches(
			&monitoringv2alpha1.WhatapServiceMonitor{},
			handler.EnqueueRequestsFromMapFunc(r.findWhatapAgents),
			builder.WithPredicates(lp),
		).
		Complete(r)
}

func getKind(obj client.Object) string {
	if obj == nil {
		return "nil"
	}
	gvk := obj.GetObjectKind().GroupVersionKind()
	if gvk.Kind != "" {
		return gvk.Kind
	}
	return fmt.Sprintf("%T", obj)
}

func loggingPredicate(logger logr.Logger) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			logger.V(1).Info("Watch Event: Create", "kind", getKind(e.Object), "name", e.Object.GetName())
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			logger.V(1).Info("Watch Event: Delete", "kind", getKind(e.Object), "name", e.Object.GetName())
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectOld.GetResourceVersion() != e.ObjectNew.GetResourceVersion() {
				logger.V(1).Info("Watch Event: Update",
					"kind", getKind(e.ObjectNew),
					"name", e.ObjectNew.GetName(),
					"diff", "ResourceVersion changed",
					"gen", fmt.Sprintf("%d->%d", e.ObjectOld.GetGeneration(), e.ObjectNew.GetGeneration()),
				)
			}
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			logger.V(1).Info("Watch Event: Generic", "kind", getKind(e.Object), "name", e.Object.GetName())
			return true
		},
	}
}

// findWhatapAgents lists all WhatapAgent CRs and returns requests for them
func (r *WhatapAgentReconciler) findWhatapAgents(ctx context.Context, obj client.Object) []reconcile.Request {
	whatapAgents := &monitoringv2alpha1.WhatapAgentList{}
	if err := r.List(ctx, whatapAgents); err != nil {
		return []reconcile.Request{}
	}
	requests := make([]reconcile.Request, len(whatapAgents.Items))
	for i, item := range whatapAgents.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(), // Use Namespace if present (WhatapAgent might be Namespaced in practice)
			},
		}
	}
	return requests
}
